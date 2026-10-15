package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

type uploadJob struct {
	localPath string
	r2Key     string
	size      int64
}

func main() {
	targetDir := flag.String("dir", "", "Local proofs directory to sync (e.g. ../../data/proofs/2026-10-15)")
	concurrency := flag.Int("concurrency", 16, "Number of concurrent upload workers")
	force := flag.Bool("force", false, "Overwrite existing objects in R2")
	flag.Parse()

	if *targetDir == "" {
		log.Fatalf("[FATAL] Missing required flag -dir. Example: go run ./cmd/sync-proofs -dir ../../data/proofs/2026-10-15")
	}

	absTarget, err := filepath.Abs(*targetDir)
	if err != nil {
		log.Fatalf("[FATAL] Invalid target directory path: %v", err)
	}

	info, err := os.Stat(absTarget)
	if err != nil || !info.IsDir() {
		log.Fatalf("[FATAL] Directory %s does not exist or is not a directory: %v", absTarget, err)
	}

	cfg := config.LoadConfig()
	if cfg.R2AccessKeyID == "" || cfg.R2SecretAccessKey == "" {
		log.Fatalf("[FATAL] R2 credentials not found in environment. Please check R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY.")
	}

	ctx, cancel := context.WithCancel(context.Background())
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
		log.Fatalf("[FATAL] Failed to initialize R2 storage: %v", err)
	}

	// Discover local proof files
	log.Printf("[INFO] Scanning local files in %s...", absTarget)
	var jobs []uploadJob
	var totalBytes int64

	err = filepath.Walk(absTarget, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		// Expect paths like: .../2026-10-15/<camera_id>/<file>.jpg
		idx := strings.Index(path, "2026-")
		if idx == -1 {
			// Fallback: use relative path from targetDir
			rel, relErr := filepath.Rel(absTarget, path)
			if relErr != nil {
				return nil
			}
			jobs = append(jobs, uploadJob{
				localPath: path,
				r2Key:     filepath.ToSlash(rel),
				size:      fi.Size(),
			})
		} else {
			jobs = append(jobs, uploadJob{
				localPath: path,
				r2Key:     filepath.ToSlash(path[idx:]),
				size:      fi.Size(),
			})
		}
		totalBytes += fi.Size()
		return nil
	})
	if err != nil {
		log.Fatalf("[FATAL] File walk failed: %v", err)
	}

	totalJobs := len(jobs)
	log.Printf("[INFO] Discovered %d files (%.2f MB) to sync to R2 bucket %q.", totalJobs, float64(totalBytes)/(1024*1024), cfg.R2BucketName)

	if totalJobs == 0 {
		log.Println("[INFO] No files found to upload. Exiting.")
		return
	}

	// Worker pool
	jobChan := make(chan uploadJob, totalJobs)
	for _, j := range jobs {
		jobChan <- j
	}
	close(jobChan)

	var uploadedCount int64
	var skippedCount int64
	var errorCount int64
	var bytesUploaded int64

	startTime := time.Now()
	var wg sync.WaitGroup

	log.Printf("[INFO] Spawning %d upload workers...", *concurrency)
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for job := range jobChan {
				// Check existence if not forcing
				if !*force {
					exists, checkErr := store.Exists(ctx, job.r2Key)
					if checkErr == nil && exists {
						atomic.AddInt64(&skippedCount, 1)
						continue
					}
				}

				f, openErr := os.Open(job.localPath)
				if openErr != nil {
					log.Printf("[WARN] [Worker %d] Failed to open %s: %v", workerID, job.localPath, openErr)
					atomic.AddInt64(&errorCount, 1)
					continue
				}

				putErr := store.Put(ctx, job.r2Key, f)
				_ = f.Close()

				if putErr != nil {
					log.Printf("[ERROR] [Worker %d] Failed to upload %s -> %s: %v", workerID, job.localPath, job.r2Key, putErr)
					atomic.AddInt64(&errorCount, 1)
				} else {
					atomic.AddInt64(&uploadedCount, 1)
					atomic.AddInt64(&bytesUploaded, job.size)
				}
			}
		}(w)
	}

	// Progress reporter goroutine
	doneChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-doneChan:
				return
			case <-ticker.C:
				up := atomic.LoadInt64(&uploadedCount)
				sk := atomic.LoadInt64(&skippedCount)
				er := atomic.LoadInt64(&errorCount)
				mb := float64(atomic.LoadInt64(&bytesUploaded)) / (1024 * 1024)
				pct := float64(up+sk+er) / float64(totalJobs) * 100.0
				log.Printf("[PROGRESS] %.1f%% (%d/%d) — Uploaded: %d (%.2f MB), Skipped: %d, Errors: %d",
					pct, up+sk+er, totalJobs, up, mb, sk, er)
			}
		}
	}()

	wg.Wait()
	close(doneChan)

	duration := time.Since(startTime)
	log.Printf("[SUCCESS] Sync completed in %v.", duration.Round(time.Millisecond))
	log.Printf("  Total files evaluated: %d", totalJobs)
	log.Printf("  Uploaded to R2:        %d (%.2f MB)", uploadedCount, float64(bytesUploaded)/(1024*1024))
	log.Printf("  Skipped (already in R2): %d", skippedCount)
	log.Printf("  Errors:                %d", errorCount)

	if errorCount > 0 {
		os.Exit(1)
	}
	fmt.Println("[DONE] All proof files are synchronized with Cloudflare R2.")
}
