package models

import (
	"encoding/json"
	"fmt"
	"os"
)

// Camera represents a single traffic camera in Ho Chi Minh City.
type Camera struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	DisplayName string   `json:"displayName"`
	Status      string   `json:"status"`
	IsOnline    bool     `json:"isOnline"`
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	SnapshotURL string   `json:"snapshotUrl"`
}

// CamerasResponse represents the API response contract for GET /api/cameras.
type CamerasResponse struct {
	Total       int      `json:"total"`
	OnlineCount int      `json:"onlineCount"`
	Cameras     []Camera `json:"cameras"`
}

// CameraStore holds loaded camera dataset and quick lookup indexes.
type CameraStore struct {
	Cameras     []Camera
	LookupMap   map[string]Camera
	Total       int
	OnlineCount int
}

// LoadCameraStore loads and parses the camera catalog from the given JSON file.
func LoadCameraStore(filePath string) (*CameraStore, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cameras file (%s): %w", filePath, err)
	}

	var cameras []Camera
	if err := json.Unmarshal(data, &cameras); err != nil {
		return nil, fmt.Errorf("failed to parse cameras json: %w", err)
	}

	lookupMap := make(map[string]Camera, len(cameras))
	onlineCount := 0
	for _, cam := range cameras {
		lookupMap[cam.ID] = cam
		if cam.IsOnline {
			onlineCount++
		}
	}

	return &CameraStore{
		Cameras:     cameras,
		LookupMap:   lookupMap,
		Total:       len(cameras),
		OnlineCount: onlineCount,
	}, nil
}
