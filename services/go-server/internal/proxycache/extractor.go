package proxycache

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type osdResult struct {
	Timestamp string `json:"timestamp"`
	RawOSD    string `json:"raw_osd"`
	Status    string `json:"status"`
}

func findExtractorPath() string {
	candidates := []string{
		"./bin/extract_osd_timestamp",
		"../../bin/extract_osd_timestamp",
		"../bin/extract_osd_timestamp",
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

// ExtractOSD runs the native extractor binary on image bytes via stdin.
func ExtractOSD(data []byte) (timestamp string, rawOSD string) {
	if len(data) == 0 || IsGhostImage(data) {
		return "", ""
	}

	binPath := findExtractorPath()
	if binPath == "" {
		return "", ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", ""
	}

	var res osdResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		return "", ""
	}

	return res.Timestamp, res.RawOSD
}
