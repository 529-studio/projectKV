package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType denotes the Prometheus metric type (gauge, counter, histogram).
type MetricType string

const (
	TypeGauge     MetricType = "gauge"
	TypeCounter   MetricType = "counter"
	TypeHistogram MetricType = "histogram"
)

// EscapeLabelValue escapes backslashes, double quotes, and newlines in Prometheus label values.
func EscapeLabelValue(val string) string {
	val = strings.ReplaceAll(val, `\`, `\\`)
	val = strings.ReplaceAll(val, `"`, `\"`)
	val = strings.ReplaceAll(val, "\n", `\n`)
	return val
}

// formatLabels formats key-value label pairs into `{k="v",...}` sorted by key.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(k)
		sb.WriteString(`="`)
		sb.WriteString(EscapeLabelValue(labels[k]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// Gauge represents a single numerical value that can arbitrarily go up and down.
type Gauge struct {
	bits uint64
}

// NewGauge creates a new Gauge initialized to 0.
func NewGauge() *Gauge {
	return &Gauge{}
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(val float64) {
	atomic.StoreUint64(&g.bits, math.Float64bits(val))
}

// Add adds the given value to the gauge.
func (g *Gauge) Add(val float64) {
	for {
		oldBits := atomic.LoadUint64(&g.bits)
		newVal := math.Float64frombits(oldBits) + val
		newBits := math.Float64bits(newVal)
		if atomic.CompareAndSwapUint64(&g.bits, oldBits, newBits) {
			return
		}
	}
}

// Get returns the current value of the gauge.
func (g *Gauge) Get() float64 {
	return math.Float64frombits(atomic.LoadUint64(&g.bits))
}

// Counter represents a monotonically increasing numerical value.
type Counter struct {
	bits uint64
}

// NewCounter creates a new Counter initialized to 0.
func NewCounter() *Counter {
	return &Counter{}
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.Add(1.0)
}

// Add adds the given value to the counter (must be >= 0).
func (c *Counter) Add(val float64) {
	if val < 0 {
		return
	}
	for {
		oldBits := atomic.LoadUint64(&c.bits)
		newVal := math.Float64frombits(oldBits) + val
		newBits := math.Float64bits(newVal)
		if atomic.CompareAndSwapUint64(&c.bits, oldBits, newBits) {
			return
		}
	}
}

// Get returns the current value of the counter.
func (c *Counter) Get() float64 {
	return math.Float64frombits(atomic.LoadUint64(&c.bits))
}

// Histogram tracks the size of events into configurable buckets.
type Histogram struct {
	mu      sync.RWMutex
	buckets []float64
	counts  []uint64
	sum     float64
	count   uint64
}

// DefaultHistogramBuckets provides typical latency thresholds in seconds.
var DefaultHistogramBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}

// NewHistogram creates a new Histogram with specified upper bound buckets.
func NewHistogram(buckets []float64) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultHistogramBuckets
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)

	return &Histogram{
		buckets: sorted,
		counts:  make([]uint64, len(sorted)),
	}
}

// Observe records a new observation in the histogram.
func (h *Histogram) Observe(val float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += val
	for i, b := range h.buckets {
		if val <= b {
			h.counts[i]++
		}
	}
}

// HistogramSnapshot returns a thread-safe snapshot of histogram data.
type HistogramSnapshot struct {
	Buckets []float64
	Counts  []uint64
	Sum     float64
	Count   uint64
}

func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	countsCopy := make([]uint64, len(h.counts))
	copy(countsCopy, h.counts)

	return HistogramSnapshot{
		Buckets: h.buckets,
		Counts:  countsCopy,
		Sum:     h.sum,
		Count:   h.count,
	}
}

// MetricCollector is implemented by any registered metric.
type MetricCollector interface {
	WriteMetric(w io.Writer, name string) error
}

type staticMetric struct {
	mType  MetricType
	help   string
	labels map[string]string
	getter func() float64
	gauge  *Gauge
	count  *Counter
}

func (sm *staticMetric) WriteMetric(w io.Writer, name string) error {
	val := sm.getter()
	lbls := formatLabels(sm.labels)
	_, err := fmt.Fprintf(w, "%s%s %g\n", name, lbls, val)
	return err
}

type histogramMetric struct {
	help   string
	labels map[string]string
	hist   *Histogram
}

func (hm *histogramMetric) WriteMetric(w io.Writer, name string) error {
	snap := hm.hist.Snapshot()
	baseLbls := hm.labels

	for i, bound := range snap.Buckets {
		bucketLabels := make(map[string]string, len(baseLbls)+1)
		for k, v := range baseLbls {
			bucketLabels[k] = v
		}
		bucketLabels["le"] = fmt.Sprintf("%g", bound)
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", name, formatLabels(bucketLabels), snap.Counts[i]); err != nil {
			return err
		}
	}

	// +Inf bucket
	infLabels := make(map[string]string, len(baseLbls)+1)
	for k, v := range baseLbls {
		infLabels[k] = v
	}
	infLabels["le"] = "+Inf"
	if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", name, formatLabels(infLabels), snap.Count); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "%s_sum%s %g\n", name, formatLabels(baseLbls), snap.Sum); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_count%s %d\n", name, formatLabels(baseLbls), snap.Count); err != nil {
		return err
	}
	return nil
}

// Registry coordinates all registered metrics and formats them for scrape.
type Registry struct {
	mu        sync.RWMutex
	startTime time.Time
	metadata  map[string]struct {
		mType MetricType
		help  string
	}
	byLabel    map[string]map[string]MetricCollector
	collectors map[string][]MetricCollector
}

// NewRegistry initializes an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		startTime: time.Now(),
		metadata: make(map[string]struct {
			mType MetricType
			help  string
		}),
		byLabel:    make(map[string]map[string]MetricCollector),
		collectors: make(map[string][]MetricCollector),
	}
}

// RegisterGauge registers or retrieves a Gauge with given labels.
func (r *Registry) RegisterGauge(name, help string, labels map[string]string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()

	lKey := formatLabels(labels)
	if byName, ok := r.byLabel[name]; ok {
		if col, exists := byName[lKey]; exists {
			if sm, ok := col.(*staticMetric); ok && sm.gauge != nil {
				return sm.gauge
			}
		}
	} else {
		r.byLabel[name] = make(map[string]MetricCollector)
	}

	r.metadata[name] = struct {
		mType MetricType
		help  string
	}{mType: TypeGauge, help: help}

	g := NewGauge()
	sm := &staticMetric{
		mType:  TypeGauge,
		help:   help,
		labels: labels,
		getter: g.Get,
		gauge:  g,
	}
	r.byLabel[name][lKey] = sm
	r.collectors[name] = append(r.collectors[name], sm)
	return g
}

// RegisterGaugeFunc registers a dynamic gauge evaluated at scrape time.
func (r *Registry) RegisterGaugeFunc(name, help string, fn func() float64, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	lKey := formatLabels(labels)
	if byName, ok := r.byLabel[name]; ok {
		if _, exists := byName[lKey]; exists {
			return
		}
	} else {
		r.byLabel[name] = make(map[string]MetricCollector)
	}

	r.metadata[name] = struct {
		mType MetricType
		help  string
	}{mType: TypeGauge, help: help}

	sm := &staticMetric{
		mType:  TypeGauge,
		help:   help,
		labels: labels,
		getter: fn,
	}
	r.byLabel[name][lKey] = sm
	r.collectors[name] = append(r.collectors[name], sm)
}

// RegisterCounter registers or retrieves a Counter with given labels.
func (r *Registry) RegisterCounter(name, help string, labels map[string]string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()

	lKey := formatLabels(labels)
	if byName, ok := r.byLabel[name]; ok {
		if col, exists := byName[lKey]; exists {
			if sm, ok := col.(*staticMetric); ok && sm.count != nil {
				return sm.count
			}
		}
	} else {
		r.byLabel[name] = make(map[string]MetricCollector)
	}

	r.metadata[name] = struct {
		mType MetricType
		help  string
	}{mType: TypeCounter, help: help}

	c := NewCounter()
	sm := &staticMetric{
		mType:  TypeCounter,
		help:   help,
		labels: labels,
		getter: c.Get,
		count:  c,
	}
	r.byLabel[name][lKey] = sm
	r.collectors[name] = append(r.collectors[name], sm)
	return c
}

// RegisterHistogram registers or retrieves a Histogram with given buckets and labels.
func (r *Registry) RegisterHistogram(name, help string, buckets []float64, labels map[string]string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()

	lKey := formatLabels(labels)
	if byName, ok := r.byLabel[name]; ok {
		if col, exists := byName[lKey]; exists {
			if hm, ok := col.(*histogramMetric); ok {
				return hm.hist
			}
		}
	} else {
		r.byLabel[name] = make(map[string]MetricCollector)
	}

	r.metadata[name] = struct {
		mType MetricType
		help  string
	}{mType: TypeHistogram, help: help}

	h := NewHistogram(buckets)
	hm := &histogramMetric{
		help:   help,
		labels: labels,
		hist:   h,
	}
	r.byLabel[name][lKey] = hm
	r.collectors[name] = append(r.collectors[name], hm)
	return h
}

// Uptime returns the seconds elapsed since registry instantiation.
func (r *Registry) Uptime() float64 {
	return time.Since(r.startTime).Seconds()
}

// WriteOpenMetrics serializes all registered metrics into OpenMetrics / Prometheus text format.
func (r *Registry) WriteOpenMetrics(w io.Writer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.metadata))
	for n := range r.metadata {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		meta := r.metadata[name]
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, meta.help); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, meta.mType); err != nil {
			return err
		}

		collectors := r.collectors[name]
		for _, c := range collectors {
			if err := c.WriteMetric(w, name); err != nil {
				return err
			}
		}
	}
	return nil
}
