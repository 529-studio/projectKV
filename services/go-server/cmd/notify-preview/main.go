package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/notify"
)

func main() {
	cameraID := flag.String("camera-id", "", "Camera ID to generate notification preview for (required)")
	severity := flag.String("severity", "FLOODED_DEEP", "Flood severity (e.g. WET, FLOODED_SHALLOW, FLOODED_DEEP)")
	observedAtStr := flag.String("observed-at", "", "Optional observation timestamp in RFC3339 format")
	flag.Parse()

	if *cameraID == "" {
		log.Fatalf("[FATAL] -camera-id is required")
	}

	cfg := config.LoadConfig()
	camerasFilePath := cfg.CamerasFilePath
	if camerasFilePath == "" {
		camerasFilePath = config.ResolvePath("data/cameras_enriched.json")
	}

	camCatalog, err := camera.LoadCatalog(camerasFilePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera catalog from %s: %v", camerasFilePath, err)
	}

	cam, ok := camCatalog.ByID(*cameraID)
	if !ok {
		log.Fatalf("[FATAL] Camera %q not found in catalog", *cameraID)
	}

	camName := *cameraID
	district := ""
	ward := ""
	snapshotURL := ""
	if cam != nil {
		if cam.DisplayName != "" {
			camName = cam.DisplayName
		} else if cam.Title != "" {
			camName = cam.Title
		}
		district = cam.Zone
		ward = cam.PhuongName
		snapshotURL = cam.SnapshotURL
	}

	chokepointsPath := config.ResolvePath("tools/eval/fixtures/flood_chokepoints.json")
	floodChokepoints, err := flood.LoadChokepoints(chokepointsPath)
	typicalDepth := 0
	if err != nil {
		log.Printf("[WARN] Failed to load chokepoints fixture from %s: %v", chokepointsPath, err)
	} else {
		for _, cp := range floodChokepoints {
			if cp.CameraID == *cameraID {
				typicalDepth = cp.TypicalDepthCm
				break
			}
		}
	}

	var observedAt time.Time
	if *observedAtStr != "" {
		parsed, err := time.Parse(time.RFC3339, *observedAtStr)
		if err != nil {
			log.Fatalf("[FATAL] Failed to parse -observed-at as RFC3339: %v", err)
		}
		observedAt = parsed
	}

	alert := notify.BuildFloodAlert(
		*cameraID,
		camName,
		district,
		ward,
		snapshotURL,
		typicalDepth,
		*severity,
		observedAt,
	)

	msg := notify.FormatTelegramMessage(alert)
	fmt.Println(msg)
	os.Exit(0)
}
