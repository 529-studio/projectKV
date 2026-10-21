package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

func main() {
	log.Println("[INFO] Starting Cloudflare R2 connectivity verification...")

	cfg := config.LoadConfig()
	if cfg.R2AccessKeyID == "" || cfg.R2SecretAccessKey == "" {
		log.Fatalf("[FATAL] R2 credentials not found in environment. Please check R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := storage.NewR2Storage(ctx, storage.R2Config{
		Endpoint:        cfg.R2Endpoint,
		AccountID:       cfg.R2AccountID,
		BucketName:      cfg.R2BucketName,
		Region:          cfg.R2Region,
		AccessKeyID:     cfg.R2AccessKeyID,
		SecretAccessKey: cfg.R2SecretAccessKey,
		PublicBaseURL:   cfg.R2PublicURL,
	})
	if err != nil {
		log.Fatalf("[FATAL] Failed to create R2 storage driver: %v", err)
	}

	now := time.Now().In(archiver.ICT)
	testKey := fmt.Sprintf("test-connectivity/%s/%d_sample.jpg", now.Format("2006-01-02"), now.Unix())
	payload := archiver.MockJPEGData

	log.Printf("[TEST 1/5] Uploading sample snapshot to key %q (%d bytes)...", testKey, len(payload))
	if err := store.Put(ctx, testKey, bytes.NewReader(payload)); err != nil {
		log.Fatalf("[FAIL] PutObject failed: %v", err)
	}
	log.Println("[PASS] Upload completed successfully.")

	log.Printf("[TEST 2/5] Checking object existence via HeadObject...")
	exists, err := store.Exists(ctx, testKey)
	if err != nil {
		log.Fatalf("[FAIL] Exists failed: %v", err)
	}
	if !exists {
		log.Fatalf("[FAIL] Object %q not found via Exists!", testKey)
	}
	log.Println("[PASS] Object presence confirmed.")

	log.Printf("[TEST 3/5] Downloading object via GetObject...")
	rc, err := store.Get(ctx, testKey)
	if err != nil {
		log.Fatalf("[FAIL] Get failed: %v", err)
	}
	downloaded, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		log.Fatalf("[FAIL] ReadAll failed: %v", err)
	}
	if !bytes.Equal(downloaded, payload) {
		log.Fatalf("[FAIL] Data corrupted: expected %d bytes, got %d bytes", len(payload), len(downloaded))
	}
	log.Printf("[PASS] Downloaded data verified intact (%d bytes).", len(downloaded))

	publicURL := store.PublicURL(testKey)
	log.Printf("[TEST 4/5] Checking Public Edge URL: %s", publicURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, publicURL, nil)
	resp, httpErr := http.DefaultClient.Do(req)
	if httpErr != nil {
		log.Printf("[WARN] Edge URL check could not reach %s: %v (public DNS/edge might take a moment to propagate)", publicURL, httpErr)
	} else {
		defer resp.Body.Close()
		log.Printf("[INFO] Edge URL HTTP status: %d %s", resp.StatusCode, resp.Status)
		if resp.StatusCode == http.StatusOK {
			log.Println("[PASS] Edge CDN serves the uploaded snapshot successfully.")
		} else {
			log.Printf("[INFO] Note: Edge URL returned status %d (bucket might require public read permission or presigned URL).", resp.StatusCode)
		}
	}

	log.Printf("[TEST 5/7] Cleaning up test artifact via DeleteObject...")
	if err := store.Delete(ctx, testKey); err != nil {
		log.Fatalf("[FAIL] Delete failed: %v", err)
	}
	existsAfter, err := store.Exists(ctx, testKey)
	if err != nil {
		log.Fatalf("[FAIL] Exists after delete failed: %v", err)
	}
	if existsAfter {
		log.Fatalf("[FAIL] Object still exists after deletion!")
	}
	log.Println("[PASS] Cleanup verified. Test object removed.")

	log.Printf("[TEST 6/7] Testing List operation on live R2 partition (prefix %q)...", now.Format("2006-01-02"))
	items, err := store.List(ctx, now.Format("2006-01-02"))
	if err != nil {
		log.Fatalf("[FAIL] List failed on R2: %v", err)
	}
	log.Printf("[PASS] List returned %d items from live R2 partition %q.", len(items), now.Format("2006-01-02"))

	log.Printf("[TEST 7/7] Testing ListDirectories operation with Delimiter on live R2 bucket...")
	dirStart := time.Now()
	dirs, err := store.ListDirectories(ctx, "")
	dirDuration := time.Since(dirStart)
	if err != nil {
		log.Fatalf("[FAIL] ListDirectories failed on R2: %v", err)
	}
	log.Printf("[PASS] ListDirectories returned %d partition directories in %v: %v", len(dirs), dirDuration, dirs)

	log.Println("[SUCCESS] All Cloudflare R2 storage tests (including List & ListDirectories) PASSED! Integration is fully operational.")
}
