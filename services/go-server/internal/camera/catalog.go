package camera

import (
	"encoding/json"
	"fmt"
	"os"
)

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

type Catalog struct {
	list  []Camera
	dict  map[string]*Camera
	zones []string
}

func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var list []Camera
	var mapData map[string]Camera

	if err := json.Unmarshal(data, &list); err != nil {
		if errMap := json.Unmarshal(data, &mapData); errMap == nil {
			for _, v := range mapData {
				list = append(list, v)
			}
		} else {
			return nil, fmt.Errorf("unmarshal json: %w", err)
		}
	}

	c := &Catalog{
		list: list,
		dict: make(map[string]*Camera),
	}

	zoneSet := make(map[string]struct{})
	for i := range c.list {
		c.dict[c.list[i].ID] = &c.list[i]
		if c.list[i].Zone != "" {
			zoneSet[c.list[i].Zone] = struct{}{}
		}
	}

	for z := range zoneSet {
		c.zones = append(c.zones, z)
	}

	return c, nil
}

func (c *Catalog) All() []Camera {
	return c.list
}

func (c *Catalog) ByID(id string) (*Camera, bool) {
	cam, ok := c.dict[id]
	return cam, ok
}

func (c *Catalog) Zones() []string {
	return c.zones
}
