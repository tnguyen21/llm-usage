package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// compact mode
	if len(os.Args) > 1 && os.Args[1] == "--compact" {
		runCompact()
		return
	}

	token, subType, err := loadToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, " ✗ %s\n   Run \"claude\" and sign in first.\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(token, subType), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCompact() {
	token, _, err := loadToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	usage, err := fetchUsage(token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	parts := []string{}
	if usage.FiveHour != nil {
		parts = append(parts, fmt.Sprintf("5h:%.0f%%", usage.FiveHour.Utilization))
	}
	if usage.SevenDay != nil {
		parts = append(parts, fmt.Sprintf("7d:%.0f%%", usage.SevenDay.Utilization))
	}

	for i, p := range parts {
		if i > 0 {
			fmt.Print(" ")
		}
		fmt.Print(p)
	}
	fmt.Println()
}
