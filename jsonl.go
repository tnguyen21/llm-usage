package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TokenStats struct {
	InputTokens    int
	OutputTokens   int
	CacheCreation  int
	CacheRead      int
}

func (t TokenStats) Total() int {
	return t.InputTokens + t.OutputTokens + t.CacheCreation + t.CacheRead
}

func claudeDataDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".claude", "projects"),
		filepath.Join(home, ".config", "claude", "projects"),
	}
	var dirs []string
	for _, d := range candidates {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func scanTokens(since time.Time) (TokenStats, error) {
	var stats TokenStats
	dirs := claudeDataDirs()
	if len(dirs) == 0 {
		return stats, fmt.Errorf("no Claude data directories found")
	}

	for _, root := range dirs {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible dirs
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".jsonl" {
				return nil
			}
			// Quick filter: skip files last modified before our window
			if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
				return nil
			}
			scanFile(path, since, &stats)
			return nil
		})
		if err != nil {
			continue
		}
	}
	return stats, nil
}

func scanFile(path string, since time.Time, stats *TokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	for scanner.Scan() {
		line := scanner.Bytes()

		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" {
			continue
		}
		if entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if ts.Before(since) {
			continue
		}

		stats.InputTokens += entry.Message.Usage.InputTokens
		stats.OutputTokens += entry.Message.Usage.OutputTokens
		stats.CacheCreation += entry.Message.Usage.CacheCreationInputTokens
		stats.CacheRead += entry.Message.Usage.CacheReadInputTokens
	}
}

func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
