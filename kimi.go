package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// KimiUsage holds Kimi rate-limit data (if available) and token stats.
type KimiUsage struct {
	// Note: Kimi doesn't appear to store rate limits locally like Codex.
	// Rate limits would need to be fetched from API if available.
}

// kimiSessionDir returns the Kimi sessions directory.
func kimiSessionDir() string {
	if home := os.Getenv("KIMI_HOME"); home != "" {
		return filepath.Join(home, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kimi", "sessions")
}

// kimiWireEntry represents a single entry in Kimi's wire.jsonl file.
type kimiWireEntry struct {
	Timestamp float64 `json:"timestamp"` // Unix timestamp as float
	Message   *struct {
		Type    string `json:"type"`
		Payload *struct {
			TokenUsage *kimiTokenUsage `json:"token_usage"`
		} `json:"payload"`
	} `json:"message"`
}

// kimiTokenUsage represents token usage data from Kimi's StatusUpdate events.
type kimiTokenUsage struct {
	InputOther         int `json:"input_other"`
	Output             int `json:"output"`
	InputCacheRead     int `json:"input_cache_read"`
	InputCacheCreation int `json:"input_cache_creation"`
}

// scanKimiTokens scans Kimi session files for token usage since the given time.
func scanKimiTokens(since time.Time) (TokenStats, error) {
	var stats TokenStats
	dir := kimiSessionDir()
	if dir == "" {
		return stats, fmt.Errorf("kimi sessions directory not found")
	}
	if _, err := os.Stat(dir); err != nil {
		return stats, nil // not installed, return zeros
	}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Check for wire.jsonl files which contain detailed token usage
		if filepath.Base(path) != "wire.jsonl" {
			return nil
		}
		// Quick filter: skip files last modified before our window
		if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
			return nil
		}
		scanKimiWireFile(path, since, &stats)
		return nil
	})
	if err != nil {
		return stats, err
	}
	return stats, nil
}

// scanKimiWireFile reads a single Kimi wire.jsonl file and adds its token usage.
// Uses the last StatusUpdate entry per message_id as the final token counts.
func scanKimiWireFile(path string, since time.Time, stats *TokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Track the last token usage for this file
	var lastUsage *kimiTokenUsage
	var lastTimestamp float64

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	for scanner.Scan() {
		var entry kimiWireEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Message == nil || entry.Message.Type != "StatusUpdate" {
			continue
		}
		if entry.Message.Payload == nil || entry.Message.Payload.TokenUsage == nil {
			continue
		}

		// Check timestamp is within our window
		if entry.Timestamp > 0 {
			ts := time.Unix(int64(entry.Timestamp), int64((entry.Timestamp-float64(int64(entry.Timestamp)))*1e9))
			if ts.Before(since) {
				continue
			}
		}

		lastUsage = entry.Message.Payload.TokenUsage
		lastTimestamp = entry.Timestamp
	}

	if lastUsage == nil {
		return
	}

	// Kimi reports cumulative usage per session
	// input_other = non-cached input tokens
	// input_cache_read = cached input tokens  
	// input_cache_creation = cache creation tokens
	// output = output tokens
	stats.InputTokens += lastUsage.InputOther
	stats.CacheRead += lastUsage.InputCacheRead
	stats.CacheCreation += lastUsage.InputCacheCreation
	stats.OutputTokens += lastUsage.Output

	_ = lastTimestamp // avoid unused variable warning
}

// scanKimiTokensByDay scans Kimi session files and buckets token usage by day of month.
func scanKimiTokensByDay(year int, month time.Month) (DailyTokenStats, error) {
	daily := make(DailyTokenStats)
	dir := kimiSessionDir()
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
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "wire.jsonl" {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
			return nil
		}
		scanKimiWireFileByDay(path, since, until, daily)
		return nil
	})
	return daily, nil
}

// scanKimiWireFileByDay reads a single Kimi wire file and buckets usage by day.
func scanKimiWireFileByDay(path string, since, until time.Time, daily DailyTokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var lastUsage *kimiTokenUsage
	var lastDay int
	var lastTS time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*512*1024)
	for scanner.Scan() {
		var entry kimiWireEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Message == nil || entry.Message.Type != "StatusUpdate" {
			continue
		}
		if entry.Message.Payload == nil || entry.Message.Payload.TokenUsage == nil {
			continue
		}

		if entry.Timestamp <= 0 {
			continue
		}
		ts := time.Unix(int64(entry.Timestamp), int64((entry.Timestamp-float64(int64(entry.Timestamp)))*1e9))
		if ts.Before(since) || !ts.Before(until) {
			continue
		}

		lastUsage = entry.Message.Payload.TokenUsage
		lastDay = ts.Day()
		lastTS = ts
	}

	if lastUsage == nil {
		return
	}

	s := daily[lastDay]
	s.InputTokens += lastUsage.InputOther
	s.CacheRead += lastUsage.InputCacheRead
	s.CacheCreation += lastUsage.InputCacheCreation
	s.OutputTokens += lastUsage.Output
	daily[lastDay] = s

	_ = lastTS
}
