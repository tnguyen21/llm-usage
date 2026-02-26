package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Load config (or use defaults)
	cfg, _ := LoadConfig()

	// compact mode
	if len(os.Args) > 1 && os.Args[1] == "--compact" {
		runCompact(cfg)
		return
	}

	token, subType, err := loadToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, " ✗ %s\n   Run \"claude\" and sign in first.\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(token, subType, cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCompact(cfg Config) {
	token, _, err := loadToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	parts := []string{}

	// Claude
	if cfg.Providers.Claude {
		usage, err := fetchUsage(token)
		if err == nil && usage != nil {
			var claudeParts []string
			if usage.FiveHour != nil {
				claudeParts = append(claudeParts, fmt.Sprintf("5h:%.0f%%", 100-usage.FiveHour.Utilization))
			}
			if usage.SevenDay != nil {
				claudeParts = append(claudeParts, fmt.Sprintf("7d:%.0f%%", 100-usage.SevenDay.Utilization))
			}
			if len(claudeParts) > 0 {
				parts = append(parts, "claude:"+joinWith(claudeParts, ","))
			}
		}
	}

	// Codex
	if cfg.Providers.Codex {
		codexUsage, codexErr := fetchCodexUsage()
		if codexErr == nil && codexUsage != nil {
			var codexParts []string
			if codexUsage.Primary != nil {
				codexParts = append(codexParts, fmt.Sprintf("5h:%.0f%%", 100-codexUsage.Primary.UsedPercent))
			}
			if codexUsage.Secondary != nil {
				codexParts = append(codexParts, fmt.Sprintf("7d:%.0f%%", 100-codexUsage.Secondary.UsedPercent))
			}
			if len(codexParts) > 0 {
				parts = append(parts, "codex:"+joinWith(codexParts, ","))
			}
		}
	}

	// Token total (always shown if any provider is enabled)
	if cfg.AnyEnabled() {
		now := time.Now()
		week, err := scanAllTokens(now.AddDate(0, 0, -7))
		if err == nil && week.Total() > 0 {
			parts = append(parts, "tok:"+formatTokenCount(week.Total()))
		}
	}

	for i, p := range parts {
		if i > 0 {
			fmt.Print(" ")
		}
		fmt.Print(p)
	}
	fmt.Println()
}

func joinWith(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
