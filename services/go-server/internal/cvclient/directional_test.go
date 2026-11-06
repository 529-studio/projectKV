package cvclient_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// TestPCUCalculator_CalculateDirectional_TwoCarriageways verifies that when one
// carriageway is jammed and the opposing carriageway is free-flowing, the overall
// camera severity is pinned to CONGESTED (worst direction), preventing jam dilution (ADR-012 D1).
func TestPCUCalculator_CalculateDirectional_TwoCarriageways(t *testing.T) {
	calc := cvclient.NewPCUCalculator(cvclient.DefaultPCUConfig())
	camID := "cam-two-carriageways"
	baseCapacity := 40.0 // 20.0 per direction at 50% capacity share

	inputs := []cvclient.DirectionalInput{
		{
			RegionID:      "cam-two-carriageways_dir_a",
			Direction:     "a",
			CapacityShare: 0.50,
			// Direction A: Jammed (35 cars + 10 trucks = 35 + 20 = 55 PCU -> SI = 55 / 20 = 2.75 >= 0.80)
			Counts: cvclient.VehicleCounts{
				Car:   35,
				Truck: 10,
			},
			WaterRatio: 0.0,
		},
		{
			RegionID:      "cam-two-carriageways_dir_b",
			Direction:     "b",
			CapacityShare: 0.50,
			// Direction B: Free-flowing (1 car + 2 motorcycles = 1.0 + 0.6 = 1.6 PCU -> SI = 1.6 / 20 = 0.08 < 0.30)
			Counts: cvclient.VehicleCounts{
				Car:        1,
				Motorcycle: 2,
			},
			WaterRatio: 0.0,
		},
	}

	out := calc.CalculateDirectional(camID, baseCapacity, inputs)

	// Overall severity MUST be pinned to the worst direction (CONGESTED), NOT diluted
	if out.WorstSeverity != hotset.SeverityCongested {
		t.Fatalf("expected worst severity %q, got %q", hotset.SeverityCongested, out.WorstSeverity)
	}

	if out.WorstSI < 2.0 {
		t.Errorf("expected worst SI >= 2.0, got %f", out.WorstSI)
	}

	if len(out.Directions) != 2 {
		t.Fatalf("expected 2 directional verdicts, got %d", len(out.Directions))
	}

	// Direction A details
	dirA := out.Directions[0]
	if dirA.Direction != "a" || dirA.Severity != hotset.SeverityCongested {
		t.Errorf("expected dir A to be CONGESTED, got direction=%s severity=%s", dirA.Direction, dirA.Severity)
	}
	if dirA.PCUTotal != 55.0 {
		t.Errorf("expected dir A PCU 55.0, got %f", dirA.PCUTotal)
	}

	// Direction B details
	dirB := out.Directions[1]
	if dirB.Direction != "b" || dirB.Severity != hotset.SeverityClear {
		t.Errorf("expected dir B to be CLEAR, got direction=%s severity=%s", dirB.Direction, dirB.Severity)
	}
	if dirB.PCUTotal != 1.6 {
		t.Errorf("expected dir B PCU 1.6, got %f", dirB.PCUTotal)
	}
}

// TestCVEngineAnalyzer_Analyze_MultipartWithRegions verifies end-to-end multipart
// serialization and directional response ingestion in CVEngineAnalyzer.
func TestCVEngineAnalyzer_Analyze_MultipartWithRegions(t *testing.T) {
	// 1. Mock proxy snapshot server
	snapServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-jpeg-frame-data"))
	}))
	defer snapServer.Close()

	var receivedContentType string
	var receivedFrameBytes []byte
	var receivedMetaJSON string

	// 2. Mock cv-engine /predict server accepting multipart/form-data
	predictServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(receivedContentType)
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			http.Error(w, "expected multipart form", http.StatusBadRequest)
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "frame":
				receivedFrameBytes = data
			case "meta":
				receivedMetaJSON = string(data)
			}
		}

		// Return directional prediction
		resp := map[string]any{
			"traffic_severity":  "MODERATE", // Frame-wide fallback
			"flood_severity":    "DRY",
			"confidence":        0.91,
			"vehicles_detected": 36,
			"vehicle_counts": map[string]int{
				"car":   35,
				"truck": 1,
			},
			"regions": []map[string]any{
				{
					"region_id":         "pilot_dir_a",
					"direction":         "a",
					"capacity_share":    0.50,
					"vehicles_detected": 35,
					"vehicle_counts": map[string]int{
						"car": 35, // 35 * 1.0 = 35 PCU -> capacity 15 -> SI 2.33 -> CONGESTED
					},
					"water_ratio": 0.0,
				},
				{
					"region_id":         "pilot_dir_b",
					"direction":         "b",
					"capacity_share":    0.50,
					"vehicles_detected": 1,
					"vehicle_counts": map[string]int{
						"car": 1, // 1 * 1.0 = 1 PCU -> capacity 15 -> SI 0.067 -> CLEAR
					},
					"water_ratio": 0.0,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer predictServer.Close()

	calc := cvclient.NewPCUCalculator(cvclient.DefaultPCUConfig())
	analyzer := cvclient.NewCVEngineAnalyzer(predictServer.URL, http.DefaultClient, cvclient.WithPCUCalculator(calc))

	cam := hotset.Camera{
		ID:           "pilot-cam-01",
		Name:         "Pilot Corridor",
		SnapshotURL:  snapServer.URL,
		BaseCapacity: 30.0,
		Regions: []hotset.RoadwayRegion{
			{
				RegionID:      "pilot_dir_a",
				Direction:     "a",
				Polygon:       [][]float64{{0.1, 0.9}, {0.4, 0.9}, {0.3, 0.4}},
				CapacityShare: 0.50,
			},
			{
				RegionID:      "pilot_dir_b",
				Direction:     "b",
				Polygon:       [][]float64{{0.6, 0.9}, {0.9, 0.9}, {0.7, 0.4}},
				CapacityShare: 0.50,
			},
		},
		DirectionCoverage: "dual",
	}

	verdict, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Assert multipart payload was received by cv-engine
	if !strings.HasPrefix(receivedContentType, "multipart/form-data") {
		t.Errorf("expected multipart/form-data, got %q", receivedContentType)
	}
	if string(receivedFrameBytes) != "fake-jpeg-frame-data" {
		t.Errorf("expected fake-jpeg-frame-data, got %q", string(receivedFrameBytes))
	}
	if !strings.Contains(receivedMetaJSON, "pilot_dir_a") {
		t.Errorf("expected meta JSON to contain pilot_dir_a, got %q", receivedMetaJSON)
	}

	// Assert overall verdict is pinned to worst direction (CONGESTED from Direction A)
	if verdict.Severity != hotset.SeverityCongested {
		t.Fatalf("expected overall severity CONGESTED, got %q", verdict.Severity)
	}

	if len(verdict.Directions) != 2 {
		t.Fatalf("expected 2 directional verdicts, got %d", len(verdict.Directions))
	}

	if verdict.Directions[0].Severity != hotset.SeverityCongested {
		t.Errorf("expected dir A CONGESTED, got %s", verdict.Directions[0].Severity)
	}
	if verdict.Directions[1].Severity != hotset.SeverityClear {
		t.Errorf("expected dir B CLEAR, got %s", verdict.Directions[1].Severity)
	}
}
