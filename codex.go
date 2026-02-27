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
	LimitID   string           `json:"limit_id"`
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

// scanCodexTokensByDay scans Codex session files and buckets token usage by day of month.
func scanCodexTokensByDay(year int, month time.Month) (DailyTokenStats, error) {
	daily := make(DailyTokenStats)
	dir := codexSessionDir()
	if dir == "" {
		return daily, nil
	}
	if _, err := os.Stat(dir); err != nil {
		return daily, nil
	}

	loc := time.Now().Location()
	since := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 1, 0)

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
			return nil
		}
		scanCodexFileTokensByDay(path, since, until, daily)
		return nil
	})
	return daily, nil
}

func scanCodexFileTokensByDay(path string, since, until time.Time, daily DailyTokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lastInfo *codexTokenInfo
	var lastTS time.Time

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
		if payload.Type != "token_count" || payload.Info == nil || payload.Info.TotalTokenUsage == nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		lastInfo = payload.Info
		lastTS = ts
	}

	if lastInfo == nil || lastInfo.TotalTokenUsage == nil {
		return
	}
	if lastTS.Before(since) || !lastTS.Before(until) {
		return
	}

	tu := lastInfo.TotalTokenUsage
	nonCached := tu.InputTokens - tu.CachedInputTokens
	if nonCached < 0 {
		nonCached = 0
	}
	day := lastTS.Day()
	s := daily[day]
	s.InputTokens += nonCached
	s.CacheRead += tu.CachedInputTokens
	s.OutputTokens += tu.OutputTokens
	daily[day] = s
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
// Codex may emit multiple rate_limits per API call with different limit_ids.
// We track per limit_id and prefer the one with actual non-zero usage data.
func parseCodexFile(path string) (*CodexUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Track last rate_limits per limit_id
	lastByID := make(map[string]*codexRateLimit)

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
		id := payload.RateLimits.LimitID
		if id == "" {
			id = "_default"
		}
		lastByID[id] = payload.RateLimits
	}

	if len(lastByID) == 0 {
		return nil, fmt.Errorf("no rate_limits in file")
	}

	// Pick the best limit_id: prefer one with non-zero usage, fall back to any.
	var lastRL *codexRateLimit
	for _, rl := range lastByID {
		pUsed := rl.Primary != nil && rl.Primary.UsedPercent > 0
		sUsed := rl.Secondary != nil && rl.Secondary.UsedPercent > 0
		if pUsed || sUsed {
			lastRL = rl
			break
		}
	}
	if lastRL == nil {
		// All zero — just pick any
		for _, rl := range lastByID {
			lastRL = rl
			break
		}
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
