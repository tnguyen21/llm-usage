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
	InputTokens   int
	OutputTokens  int
	CacheCreation int
	CacheRead     int
}

func (t TokenStats) Total() int {
	return t.InputTokens + t.OutputTokens + t.CacheCreation + t.CacheRead
}

// DailyTokenStats maps day-of-month (1-31) to TokenStats.
type DailyTokenStats map[int]TokenStats

func claudeSessionDirs() []string {
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

func scanClaudeTokens(since time.Time) (TokenStats, error) {
	var stats TokenStats
	dirs := claudeSessionDirs()
	if len(dirs) == 0 {
		return stats, nil
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
			scanClaudeFileTokens(path, since, &stats)
			return nil
		})
		if err != nil {
			continue
		}
	}
	return stats, nil
}

// scanAllTokens combines Claude + Codex token counts.
func scanAllTokens(since time.Time) (TokenStats, error) {
	claude, err := scanClaudeTokens(since)
	if err != nil {
		return claude, err
	}
	codex, err := scanCodexTokens(since)
	if err != nil {
		return claude, err
	}
	return TokenStats{
		InputTokens:  claude.InputTokens + codex.InputTokens,
		OutputTokens: claude.OutputTokens + codex.OutputTokens,
		CacheCreation: claude.CacheCreation + codex.CacheCreation,
		CacheRead:    claude.CacheRead + codex.CacheRead,
	}, nil
}

func scanClaudeFileTokens(path string, since time.Time, stats *TokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Claude Code writes multiple JSONL entries per streamed message (same
	// message ID, cumulative usage). We must deduplicate: keep only the last
	// entry per message ID which holds the final token counts.
	type usage struct {
		in, out, cacheCreate, cacheRead int
	}
	seen := make(map[string]usage)  // message ID -> final usage
	var anonymous []usage           // entries without a message ID

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

		u := usage{
			in:          entry.Message.Usage.InputTokens,
			out:         entry.Message.Usage.OutputTokens,
			cacheCreate: entry.Message.Usage.CacheCreationInputTokens,
			cacheRead:   entry.Message.Usage.CacheReadInputTokens,
		}

		if entry.Message.ID != "" {
			seen[entry.Message.ID] = u // last write wins
		} else {
			anonymous = append(anonymous, u)
		}
	}

	for _, u := range seen {
		stats.InputTokens += u.in
		stats.OutputTokens += u.out
		stats.CacheCreation += u.cacheCreate
		stats.CacheRead += u.cacheRead
	}
	for _, u := range anonymous {
		stats.InputTokens += u.in
		stats.OutputTokens += u.out
		stats.CacheCreation += u.cacheCreate
		stats.CacheRead += u.cacheRead
	}
}

// scanClaudeTokensByDay scans Claude JSONL files and buckets token usage by day of month.
func scanClaudeTokensByDay(year int, month time.Month) (DailyTokenStats, error) {
	daily := make(DailyTokenStats)
	dirs := claudeSessionDirs()
	if len(dirs) == 0 {
		return daily, nil
	}

	loc := time.Now().Location()
	since := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 1, 0)

	for _, root := range dirs {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().Before(since) {
				return nil
			}
			scanClaudeFileTokensByDay(path, since, until, daily)
			return nil
		})
	}
	return daily, nil
}

func scanClaudeFileTokensByDay(path string, since, until time.Time, daily DailyTokenStats) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	type usage struct {
		in, out, cacheCreate, cacheRead int
		day                             int
	}
	seen := make(map[string]usage)
	var anonymous []usage

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)
	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil || ts.Before(since) || !ts.Before(until) {
			continue
		}

		u := usage{
			in:          entry.Message.Usage.InputTokens,
			out:         entry.Message.Usage.OutputTokens,
			cacheCreate: entry.Message.Usage.CacheCreationInputTokens,
			cacheRead:   entry.Message.Usage.CacheReadInputTokens,
			day:         ts.Day(),
		}
		if entry.Message.ID != "" {
			seen[entry.Message.ID] = u
		} else {
			anonymous = append(anonymous, u)
		}
	}

	add := func(u usage) {
		s := daily[u.day]
		s.InputTokens += u.in
		s.OutputTokens += u.out
		s.CacheCreation += u.cacheCreate
		s.CacheRead += u.cacheRead
		daily[u.day] = s
	}
	for _, u := range seen {
		add(u)
	}
	for _, u := range anonymous {
		add(u)
	}
}

// scanAllTokensByDay combines Claude + Codex per-day token counts.
func scanAllTokensByDay(year int, month time.Month) (DailyTokenStats, error) {
	claude, err := scanClaudeTokensByDay(year, month)
	if err != nil {
		return claude, err
	}
	codex, err := scanCodexTokensByDay(year, month)
	if err != nil {
		return claude, err
	}
	for day, cs := range codex {
		s := claude[day]
		s.InputTokens += cs.InputTokens
		s.OutputTokens += cs.OutputTokens
		s.CacheCreation += cs.CacheCreation
		s.CacheRead += cs.CacheRead
		claude[day] = s
	}
	return claude, nil
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
