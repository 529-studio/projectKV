package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	_ "modernc.org/sqlite"
)

type AuditStats struct {
	TotalActive   int
	ValidActive   int
	InvalidActive int
	ByTargetType  map[string]int
	InvalidRows   []InvalidRow
	Quarantined   int
	ByReason      map[string]int
}

type InvalidRow struct {
	ID         int64
	Channel    string
	ChatID     string
	TargetType string
	TargetID   string
	Reason     string
}

func main() {
	dbPath := flag.String("db", "", "Path to SQLite database (defaults to config DBPath or data/traffic.db)")
	catalogPath := flag.String("cameras", "", "Path to camera catalog JSON (defaults to data/cameras_enriched.json)")
	strict := flag.Bool("strict", true, "Exit with non-zero code if invalid subscriptions are found")
	flag.Parse()

	resolvedDB := *dbPath
	if resolvedDB == "" {
		resolvedDB = config.ResolvePath("data/traffic.db")
	}

	resolvedCatalog := *catalogPath
	if resolvedCatalog == "" {
		if _, err := os.Stat("cameras_enriched.json"); err == nil {
			resolvedCatalog = "cameras_enriched.json"
		} else {
			resolvedCatalog = config.ResolvePath("data/cameras_enriched.json")
		}
	}

	cat, err := camera.LoadCatalog(resolvedCatalog)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera catalog from %s: %v", resolvedCatalog, err)
	}

	db, err := sql.Open("sqlite", resolvedDB)
	if err != nil {
		log.Fatalf("[FATAL] Failed to open SQLite database %s: %v", resolvedDB, err)
	}
	defer db.Close()

	stats, err := RunAudit(db, cat)
	if err != nil {
		log.Fatalf("[FATAL] Audit execution failed: %v", err)
	}

	PrintReport(resolvedDB, stats)

	if *strict && stats.InvalidActive > 0 {
		os.Exit(1)
	}
}

func RunAudit(db *sql.DB, cat *camera.Catalog) (*AuditStats, error) {
	stats := &AuditStats{
		ByTargetType: make(map[string]int),
		ByReason:     make(map[string]int),
	}

	// 1. Audit active flood_subscriptions
	rows, err := db.Query(`
		SELECT id, channel_name, chat_id, target_type, target_id
		FROM flood_subscriptions
		ORDER BY id ASC
	`)
	if err != nil {
		// Table might not exist yet if database is unmigrated
		return stats, fmt.Errorf("query flood_subscriptions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		stats.TotalActive++
		var id int64
		var channel, chatID, targetType, targetID string
		if err := rows.Scan(&id, &channel, &chatID, &targetType, &targetID); err != nil {
			return nil, fmt.Errorf("scan subscription row: %w", err)
		}

		stats.ByTargetType[targetType]++

		valid, reason := ValidateTarget(cat, targetType, targetID)
		if valid {
			stats.ValidActive++
		} else {
			stats.InvalidActive++
			stats.InvalidRows = append(stats.InvalidRows, InvalidRow{
				ID:         id,
				Channel:    channel,
				ChatID:     chatID,
				TargetType: targetType,
				TargetID:   targetID,
				Reason:     reason,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flood_subscriptions: %w", err)
	}

	// 2. Audit quarantined legacy rows in flood_subscriptions_legacy_0010 (if table exists)
	var legacyTableExists int
	_ = db.QueryRow(`
		SELECT count(*) FROM sqlite_master WHERE type='table' AND name='flood_subscriptions_legacy_0010'
	`).Scan(&legacyTableExists)

	if legacyTableExists > 0 {
		lRows, err := db.Query(`
			SELECT quarantine_reason, count(*)
			FROM flood_subscriptions_legacy_0010
			GROUP BY quarantine_reason
		`)
		if err == nil {
			defer lRows.Close()
			for lRows.Next() {
				var reason string
				var count int
				if err := lRows.Scan(&reason, &count); err == nil {
					stats.Quarantined += count
					stats.ByReason[reason] = count
				}
			}
		}
	}

	return stats, nil
}

func ValidateTarget(cat *camera.Catalog, targetType, targetID string) (bool, string) {
	switch subscription.TargetType(targetType) {
	case subscription.TargetAll:
		return true, ""
	case subscription.TargetZone:
		if strings.TrimSpace(targetID) == "" {
			return false, "empty_zone_target_id"
		}
		for _, z := range subscription.CanonicalZones {
			if strings.EqualFold(z, targetID) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("non_canonical_zone: %q", targetID)
	case subscription.TargetWard:
		trimmed := strings.TrimSpace(targetID)
		if trimmed == "" {
			return false, "empty_ward_target_id"
		}
		if cat != nil {
			for _, p := range cat.Phuongs() {
				if strings.EqualFold(p.Name, trimmed) || strings.EqualFold(p.Type+" "+p.Name, trimmed) {
					return true, ""
				}
			}
			// Check prefix-less
			target := trimmed
			for _, prefix := range []string{"phường ", "xã ", "thị trấn "} {
				if strings.HasPrefix(strings.ToLower(target), prefix) {
					target = strings.TrimSpace(target[len(prefix):])
					break
				}
			}
			for _, p := range cat.Phuongs() {
				if strings.EqualFold(p.Name, target) {
					return true, ""
				}
			}
		}
		return false, fmt.Sprintf("unknown_ward: %q", targetID)
	case subscription.TargetChokepoint:
		trimmed := strings.TrimSpace(targetID)
		if trimmed == "" {
			return false, "empty_chokepoint_target_id"
		}
		if cat != nil {
			if _, ok := cat.ByID(trimmed); ok {
				return true, ""
			}
		}
		return false, fmt.Sprintf("unknown_camera_id: %q", targetID)
	default:
		return false, fmt.Sprintf("invalid_target_type: %q", targetType)
	}
}

func PrintReport(dbPath string, stats *AuditStats) {
	fmt.Println("=== SUBSCRIPTION TAXONOMY AUDIT REPORT ===")
	fmt.Printf("Database: %s\n\n", dbPath)

	fmt.Println("Active Subscriptions:")
	fmt.Printf("  Total Active:     %d\n", stats.TotalActive)
	fmt.Printf("  Valid Active:     %d\n", stats.ValidActive)
	fmt.Printf("  Invalid Active:   %d\n\n", stats.InvalidActive)

	fmt.Println("Active Breakdown by Target Type:")
	for _, tt := range subscription.CanonicalTargetTypes {
		fmt.Printf("  - %-12s: %d\n", tt, stats.ByTargetType[tt])
	}
	fmt.Println()

	fmt.Println("Quarantined Legacy Subscriptions (0010):")
	fmt.Printf("  Total Quarantined: %d\n", stats.Quarantined)
	if len(stats.ByReason) > 0 {
		for reason, count := range stats.ByReason {
			fmt.Printf("  - %-32s: %d\n", reason, count)
		}
	}
	fmt.Println()

	if len(stats.InvalidRows) > 0 {
		fmt.Println("Invalid Active Rows (Must be 0 post-migration):")
		for _, row := range stats.InvalidRows {
			fmt.Printf("  ID: %d | Chat: %s | Type: %s | Target: %q | Reason: %s\n",
				row.ID, row.ChatID, row.TargetType, row.TargetID, row.Reason)
		}
		fmt.Println()
		fmt.Println("STATUS: FAILED (Non-canonical active targets found)")
	} else {
		fmt.Println("STATUS: PASSED (Zero non-canonical active targets)")
	}
	fmt.Println("==========================================")
}
