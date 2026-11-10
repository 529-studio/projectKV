package forecast

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed embedded/prototype_v1.json
var defaultArtifactBytes []byte

var (
	ErrArtifactNotFound      = errors.New("model artifact not found")
	ErrArtifactInvalid       = errors.New("invalid model artifact format")
	ErrArtifactPathUntrusted = errors.New("untrusted or unsafe model artifact path")
	ErrArtifactTooLarge      = errors.New("model artifact exceeds maximum allowed size")
)

const (
	// MaxArtifactSizeBytes defines a safe 10MB ceiling to prevent memory exhaustion on public endpoints.
	MaxArtifactSizeBytes = 10 * 1024 * 1024
)

// ModelArtifact defines the top-level container for multi-horizon forecast models.
type ModelArtifact struct {
	SchemaVersion  int          `json:"schema_version"`
	GeneratedAtUTC string       `json:"generated_at_utc,omitempty"`
	FitSource      string       `json:"fit_source,omitempty"`
	Models         []ModelEntry `json:"models"`

	// SHA256 holds the computed cryptographic digest of the raw artifact data.
	SHA256 string `json:"-"`
}

// ModelEntry represents a single candidate or baseline model inside the artifact.
type ModelEntry struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	TargetMode     string         `json:"target_mode"`
	FitSource      FitSource      `json:"fit_source"`
	FitRows        int            `json:"fit_rows"`
	FitCameras     int            `json:"fit_cameras"`
	FitWindow      *FitWindow     `json:"fit_window"`
	FitDataSHA256  string         `json:"fit_data_sha256"`
	ArtifactSHA256 string         `json:"artifact_sha256,omitempty"`
	SHA256         string         `json:"sha256,omitempty"`
	MAE            float64        `json:"mae,omitempty"`
	GlobalMedian   float64        `json:"global_median,omitempty"`
	Horizons       []HorizonModel `json:"horizons"`
}

// HorizonModel associates a horizon metric with its calibrated parameters.
type HorizonModel struct {
	HorizonMinutes int          `json:"horizon_minutes"`
	MAE            float64      `json:"mae"`
	RMSE           float64      `json:"rmse"`
	SampleCount    int          `json:"sample_count"`
	Params         *ModelParams `json:"params,omitempty"`
}

var (
	defaultArtifactOnce sync.Once
	defaultArtifact     *ModelArtifact
	defaultArtifactErr  error
)

// DefaultArtifact returns the parsed embedded model artifact.
func DefaultArtifact() *ModelArtifact {
	defaultArtifactOnce.Do(func() {
		defaultArtifact, defaultArtifactErr = LoadArtifactFromBytes(defaultArtifactBytes)
	})
	if defaultArtifactErr != nil {
		panic(fmt.Sprintf("failed to parse embedded model artifact: %v", defaultArtifactErr))
	}
	return defaultArtifact
}

// LoadArtifact loads and validates a ModelArtifact from a path on disk.
// If path is empty, it returns the embedded default model artifact.
func LoadArtifact(path string) (*ModelArtifact, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return DefaultArtifact(), nil
	}

	// Security: clean and validate path to prevent path manipulation.
	cleaned := filepath.Clean(cleanPath)
	if !strings.HasSuffix(strings.ToLower(cleaned), ".json") {
		return nil, fmt.Errorf("%w: path must end in .json", ErrArtifactPathUntrusted)
	}

	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrArtifactNotFound, cleaned)
		}
		return nil, fmt.Errorf("stat artifact file: %w", err)
	}

	if info.Size() > MaxArtifactSizeBytes {
		return nil, fmt.Errorf("%w: size %d exceeds %d bytes", ErrArtifactTooLarge, info.Size(), MaxArtifactSizeBytes)
	}

	f, err := os.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("open artifact file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxArtifactSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read artifact file: %w", err)
	}
	if int64(len(data)) > MaxArtifactSizeBytes {
		return nil, ErrArtifactTooLarge
	}

	return LoadArtifactFromBytes(data)
}

// LoadArtifactFromBytes decodes and validates a ModelArtifact from raw JSON bytes,
// computing the real cryptographic SHA256 checksum dynamically.
func LoadArtifactFromBytes(data []byte) (*ModelArtifact, error) {
	computedSHA := fmt.Sprintf("%x", sha256.Sum256(data))

	var artifact ModelArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArtifactInvalid, err)
	}

	artifact.SHA256 = computedSHA

	// Populate SHA256 on entries and link parameters
	for i := range artifact.Models {
		artifact.Models[i].ArtifactSHA256 = computedSHA
		artifact.Models[i].SHA256 = computedSHA
		for j := range artifact.Models[i].Horizons {
			h := &artifact.Models[i].Horizons[j]
			if h.Params != nil {
				h.Params.HorizonMinutes = h.HorizonMinutes
				h.Params.MAE = h.MAE
				h.Params.RMSE = h.RMSE
			}
		}
	}

	if err := artifact.Validate(); err != nil {
		return nil, err
	}

	return &artifact, nil
}

// Validate asserts integrity of the artifact according to ADR-012 and spec §3.6.
func (a *ModelArtifact) Validate() error {
	if a == nil {
		return fmt.Errorf("%w: artifact is nil", ErrArtifactInvalid)
	}
	if a.SchemaVersion <= 0 {
		return fmt.Errorf("%w: schema_version must be positive", ErrArtifactInvalid)
	}
	if a.FitSource != "" {
		if a.FitSource != string(FitSourceLiveHistory) && a.FitSource != string(FitSourceSyntheticFixture) {
			return fmt.Errorf("%w: invalid top-level fit_source %q", ErrArtifactInvalid, a.FitSource)
		}
	}
	if len(a.Models) == 0 {
		return fmt.Errorf("%w: artifact must contain at least one model", ErrArtifactInvalid)
	}

	foundRidge := false
	for _, m := range a.Models {
		if a.FitSource != "" && string(m.FitSource) != a.FitSource {
			return fmt.Errorf("%w: model %q fit_source %q conflicts with top-level fit_source %q",
				ErrArtifactInvalid, m.Name, m.FitSource, a.FitSource)
		}
		if m.Name == ModelRidgeDelta {
			foundRidge = true
			covered := make(map[int]bool, len(m.Horizons))
			for _, h := range m.Horizons {
				if h.Params == nil {
					return fmt.Errorf("%w: candidate model %q missing parameters for horizon %d",
						ErrArtifactInvalid, ModelRidgeDelta, h.HorizonMinutes)
				}
				covered[h.HorizonMinutes] = true
			}
			for _, reqH := range SupportedHorizons {
				if !covered[reqH] {
					return fmt.Errorf("%w: candidate model %q missing required horizon %d (must cover all %v)",
						ErrArtifactInvalid, ModelRidgeDelta, reqH, SupportedHorizons)
				}
			}
		}
		if m.FitSource != FitSourceLiveHistory && m.FitSource != FitSourceSyntheticFixture {
			return fmt.Errorf("%w: invalid fit_source %q for model %q", ErrArtifactInvalid, m.FitSource, m.Name)
		}
		if m.FitSource == FitSourceLiveHistory {
			if m.FitRows <= 0 {
				return fmt.Errorf("%w: model %q with fit_source live_history must have positive fit_rows", ErrArtifactInvalid, m.Name)
			}
			if m.FitWindow == nil || m.FitWindow.Start == "" || m.FitWindow.End == "" {
				return fmt.Errorf("%w: model %q with fit_source live_history must specify fit_window", ErrArtifactInvalid, m.Name)
			}
		}
		if len(m.Horizons) == 0 {
			return fmt.Errorf("%w: model %q has no horizons defined", ErrArtifactInvalid, m.Name)
		}
		for _, h := range m.Horizons {
			if !IsSupportedHorizon(h.HorizonMinutes) {
				return fmt.Errorf("%w: unsupported horizon %d in model %q", ErrArtifactInvalid, h.HorizonMinutes, m.Name)
			}
		}
	}

	if !foundRidge {
		return fmt.Errorf("%w: artifact must contain candidate model %q", ErrArtifactInvalid, ModelRidgeDelta)
	}

	return nil
}

// Entry finds a model by name in the artifact.
func (a *ModelArtifact) Entry(name string) (ModelEntry, bool) {
	if a == nil {
		return ModelEntry{}, false
	}
	for _, m := range a.Models {
		if m.Name == name {
			return m, true
		}
	}
	return ModelEntry{}, false
}

// Provenance converts a ModelEntry into the public ModelMetadata API structure.
func (e ModelEntry) Provenance() ModelMetadata {
	metrics := make([]HorizonMetric, len(e.Horizons))
	var totalMAE float64
	for i, h := range e.Horizons {
		metrics[i] = HorizonMetric{
			HorizonMinutes: h.HorizonMinutes,
			MAE:            h.MAE,
			RMSE:           h.RMSE,
			SampleCount:    h.SampleCount,
		}
		totalMAE += h.MAE
	}

	modelMAE := e.MAE
	if modelMAE == 0 && len(e.Horizons) > 0 {
		modelMAE = e.Horizons[0].MAE
	}

	return ModelMetadata{
		Name:           e.Name,
		Version:        e.Version,
		TargetMode:     e.TargetMode,
		FitSource:      string(e.FitSource),
		Validated:      e.FitSource == FitSourceLiveHistory,
		FitRows:        e.FitRows,
		FitCameras:     e.FitCameras,
		FitWindow:      e.FitWindow,
		FitDataSHA256:  e.FitDataSHA256,
		ArtifactSHA256: e.ArtifactSHA256,
		SHA256:         e.SHA256,
		MAE:            modelMAE,
		Metrics:        metrics,
	}
}

// Params retrieves the calibrated parameters for a specific horizon.
func (e ModelEntry) Params(horizon int) (ModelParams, bool) {
	for _, h := range e.Horizons {
		if h.HorizonMinutes == horizon {
			if h.Params != nil {
				return *h.Params, true
			}
			return ModelParams{
				HorizonMinutes: horizon,
				MAE:            h.MAE,
				RMSE:           h.RMSE,
				GlobalMedian:   e.GlobalMedian,
			}, true
		}
	}
	return ModelParams{}, false
}

// RidgeParams builds the map of ModelParams for the ridge_delta candidate model.
func (a *ModelArtifact) RidgeParams() map[int]ModelParams {
	out := make(map[int]ModelParams)
	entry, ok := a.Entry(ModelRidgeDelta)
	if !ok {
		return out
	}
	for _, h := range entry.Horizons {
		if h.Params != nil {
			out[h.HorizonMinutes] = *h.Params
		}
	}
	return out
}

// DefaultMetadata returns the default provenance for ridge_delta.
func (a *ModelArtifact) DefaultMetadata() ModelMetadata {
	entry, ok := a.Entry(ModelRidgeDelta)
	if !ok {
		return ModelMetadata{
			Name:       ModelRidgeDelta,
			FitSource:  string(FitSourceSyntheticFixture),
			Validated:  false,
			TargetMode: "delta",
		}
	}
	return entry.Provenance()
}
