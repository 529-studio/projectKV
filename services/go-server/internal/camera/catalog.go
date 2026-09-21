package camera

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Reader defines query capabilities on the camera catalog.
// (Interface Segregation Principle: callers only depend on what they read)
type Reader interface {
	All() []Camera
	ByID(id string) (*Camera, bool)
	Zones() []string
	Filter(zone, corridor string) []Camera
}

// Camera represents a single traffic camera entity.
type Camera struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	DisplayName string  `json:"displayName"`
	Status      string  `json:"status"`
	IsOnline    bool    `json:"isOnline"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	SnapshotURL string  `json:"snapshotUrl"`
	Zone        string  `json:"zone,omitempty"`
}

// Catalog holds an in-memory repository of traffic cameras with fast index lookups.
type Catalog struct {
	list  []Camera
	dict  map[string]*Camera
	zones []string
}

// Ensure Catalog implements Reader.
var _ Reader = (*Catalog)(nil)

// NewCatalog constructs a Catalog using the default zone classifier.
func NewCatalog(cameras []Camera) *Catalog {
	return NewCatalogWithClassifier(cameras, NewDefaultZoneClassifier())
}

// NewCatalogWithClassifier constructs a Catalog with an injected ZoneClassifier.
// (Dependency Inversion Principle: Catalog depends on ZoneClassifier abstraction)
func NewCatalogWithClassifier(cameras []Camera, classifier ZoneClassifier) *Catalog {
	if classifier == nil {
		classifier = NewDefaultZoneClassifier()
	}

	c := &Catalog{
		list: make([]Camera, len(cameras)),
		dict: make(map[string]*Camera, len(cameras)),
	}

	copy(c.list, cameras)

	zoneSet := make(map[string]struct{})
	for i := range c.list {
		if c.list[i].Zone == "" {
			c.list[i].Zone = classifier.Classify(c.list[i])
		}
		c.dict[c.list[i].ID] = &c.list[i]
		if c.list[i].Zone != "" {
			zoneSet[c.list[i].Zone] = struct{}{}
		}
	}

	c.zones = make([]string, 0, len(zoneSet))
	for z := range zoneSet {
		c.zones = append(c.zones, z)
	}
	sort.Strings(c.zones)

	return c
}

// LoadCatalog reads camera JSON data from disk using the default zone classifier.
func LoadCatalog(path string) (*Catalog, error) {
	return LoadCatalogWithClassifier(path, NewDefaultZoneClassifier())
}

// LoadCatalogWithClassifier reads camera JSON data from disk and uses the provided classifier.
func LoadCatalogWithClassifier(path string, classifier ZoneClassifier) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var list []Camera
	if err := json.Unmarshal(data, &list); err == nil {
		return NewCatalogWithClassifier(list, classifier), nil
	}

	// Try object envelope {"cameras": [...]}
	var envelope struct {
		Cameras []Camera `json:"cameras"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Cameras) > 0 {
		return NewCatalogWithClassifier(envelope.Cameras, classifier), nil
	}

	// Try map[string]Camera
	var mapData map[string]Camera
	if err := json.Unmarshal(data, &mapData); err == nil && len(mapData) > 0 {
		list = make([]Camera, 0, len(mapData))
		for _, v := range mapData {
			list = append(list, v)
		}
		return NewCatalogWithClassifier(list, classifier), nil
	}

	return nil, fmt.Errorf("unmarshal json: %s", ErrInvalidCatalogJSON)
}

// All returns a slice of all cameras in the catalog.
func (c *Catalog) All() []Camera {
	res := make([]Camera, len(c.list))
	copy(res, c.list)
	return res
}

// ByID returns a camera pointer and a boolean flag indicating if found.
func (c *Catalog) ByID(id string) (*Camera, bool) {
	cam, ok := c.dict[id]
	return cam, ok
}

// Zones returns a sorted slice of unique zone identifiers.
func (c *Catalog) Zones() []string {
	res := make([]string, len(c.zones))
	copy(res, c.zones)
	return res
}

// Filter returns cameras matching optional zone and corridor parameters.
// Zone matching is case-insensitive.
// Corridor matching checks if DisplayName or Title contains the corridor keyword (case-insensitive).
func (c *Catalog) Filter(zone, corridor string) []Camera {
	res := make([]Camera, 0, len(c.list))
	zone = strings.TrimSpace(zone)
	corridor = strings.ToLower(strings.TrimSpace(corridor))

	for _, cam := range c.list {
		if zone != "" && !strings.EqualFold(cam.Zone, zone) {
			continue
		}
		if corridor != "" {
			name := strings.ToLower(cam.DisplayName)
			title := strings.ToLower(cam.Title)
			if !strings.Contains(name, corridor) && !strings.Contains(title, corridor) {
				continue
			}
		}
		res = append(res, cam)
	}
	return res
}
