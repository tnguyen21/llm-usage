package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CodexUsage holds Codex rate-limit data parsed from local session files.
type CodexUsage struct {
	Primary   *CodexBucket
	Secondary *CodexBucket
}

// CodexBucket represents one rate-limit window (primary=5h, secondary=weekly).
type CodexBucket struct {
	UsedPercent   float64
	WindowMinutes int
	ResetsAt      int64 // Unix timestamp
}

// ResetsAtTime converts the Unix timestamp to a time.Time.
func (b *CodexBucket) ResetsAtTime() time.Time {
	return time.Unix(b.ResetsAt, 0)
}

// codex JSONL structures

type codexJSONLEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	Type       string          `json:"type"`
	RateLimits *codexRateLimit `json:"rate_limits"`
}

type codexRateLimit struct {
	Primary   *codexBucketJSON `json:"primary"`
	Secondary *codexBucketJSON `json:"secondary"`
}

type codexBucketJSON struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func codexSessionDir() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// fetchCodexUsage scans the most recent Codex session files for rate_limits.
func fetchCodexUsage() (*CodexUsage, error) {
	dir := codexSessionDir()
	if dir == "" {
		return nil, fmt.Errorf("codex sessions directory not found")
	}
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("codex not installed: %w", err)
	}

	// Find all jsonl files and sort by modification time (newest first)
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan codex sessions: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no codex session files found")
	}

	sort.Slice(files, func(i, j int) bool {
		iInfo, _ := os.Stat(files[i])
		jInfo, _ := os.Stat(files[j])
		if iInfo == nil || jInfo == nil {
			return false
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	// Check the 5 most recent files for rate_limits
	limit := 5
	if len(files) < limit {
		limit = len(files)
	}

	for _, f := range files[:limit] {
		usage, err := parseCodexFile(f)
		if err == nil && usage != nil {
			return usage, nil
		}
	}

	return nil, fmt.Errorf("no rate limit data found in recent codex sessions")
}

// scanCodexTokens scans Codex session files for token usage since the given time.
func scanCodexTokens(since time.Time) (TokenStats, error) {
	var stats TokenStats
	dir := codexSessionDir()
	if dir == "" {
		return stats, fmt.Errorf("codex sessions directory not found")
	}
	if _, err := os.Stat(dir); err != nil {
		return stats, nil // not installed, return zeros
	}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
			return nil
		}
		scanCodexFileTokens(path, since, &stats)
		return nil
	})
	if err != nil {
		return stats, err
	}
	return stats, nil
}

// scanCodexFileTokens reads a single Codex session file and adds its token usage.
// Uses the last total_token_usage entry as the session total.
func scanCodexFileTokens(path string, since time.Time, stats *TokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lastInfo *codexTokenInfo
	var lastTimestamp string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	for scanner.Scan() {
		var entry codexJSONLEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "event_msg" || entry.Payload == nil {
			continue
		}

		var payload codexTokenPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			continue
		}
		if payload.Type != "token_count" || payload.Info == nil {
			continue
		}
		if payload.Info.TotalTokenUsage == nil {
			continue
		}

		lastInfo = payload.Info
		lastTimestamp = entry.Timestamp
	}

	if lastInfo == nil || lastInfo.TotalTokenUsage == nil {
		return
	}

	// Check timestamp is within our window
	if lastTimestamp != "" {
		ts, err := time.Parse(time.RFC3339Nano, lastTimestamp)
		if err == nil && ts.Before(since) {
			return
		}
	}

	tu := lastInfo.TotalTokenUsage
	// input_tokens includes cached, so non-cached = input - cached
	nonCached := tu.InputTokens - tu.CachedInputTokens
	if nonCached < 0 {
		nonCached = 0
	}
	stats.InputTokens += nonCached
	stats.CacheRead += tu.CachedInputTokens
	stats.OutputTokens += tu.OutputTokens
	// CacheCreation stays 0 for Codex (no equivalent field)
}

type codexTokenPayload struct {
	Type string         `json:"type"`
	Info *codexTokenInfo `json:"info"`
}

type codexTokenInfo struct {
	TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
}

type codexTokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

// parseCodexFile reads a single session file and returns the last rate_limits entry.
func parseCodexFile(path string) (*CodexUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lastRL *codexRateLimit

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	for scanner.Scan() {
		var entry codexJSONLEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "event_msg" {
			continue
		}
		if entry.Payload == nil {
			continue
		}

		var payload codexPayload
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			continue
		}
		if payload.Type != "token_count" || payload.RateLimits == nil {
			continue
		}
		lastRL = payload.RateLimits
	}

	if lastRL == nil {
		return nil, fmt.Errorf("no rate_limits in file")
	}

	now := time.Now()
	usage := &CodexUsage{}
	if lastRL.Primary != nil {
		b := &CodexBucket{
			UsedPercent:   lastRL.Primary.UsedPercent,
			WindowMinutes: lastRL.Primary.WindowMinutes,
			ResetsAt:      lastRL.Primary.ResetsAt,
		}
		if b.ResetsAt > 0 && b.ResetsAtTime().Before(now) {
			b.UsedPercent = 0
			b.ResetsAt = 0
		}
		usage.Primary = b
	}
	if lastRL.Secondary != nil {
		b := &CodexBucket{
			UsedPercent:   lastRL.Secondary.UsedPercent,
			WindowMinutes: lastRL.Secondary.WindowMinutes,
			ResetsAt:      lastRL.Secondary.ResetsAt,
		}
		if b.ResetsAt > 0 && b.ResetsAtTime().Before(now) {
			b.UsedPercent = 0
			b.ResetsAt = 0
		}
		usage.Secondary = b
	}

	return usage, nil
}
