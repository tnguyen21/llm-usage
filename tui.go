package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// messages

type usageFetchedMsg struct {
	usage *UsageResponse
	err   error
}

type tickMsg time.Time

type tokensFetchedMsg struct {
	today TokenStats
	week  TokenStats
	err   error
}

// styles

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	labelColor = lipgloss.Color("252")

	resetColor = lipgloss.Color("243")

	percentStyle = lipgloss.NewStyle().
			Width(6).
			Align(lipgloss.Right).
			Foreground(lipgloss.Color("252"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	staleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243"))
)

// model

type model struct {
	usage     *UsageResponse
	err       error
	lastFetch time.Time
	stale     bool

	sessionBar progress.Model
	weeklyBar  progress.Model
	opusBar    progress.Model
	spinner    spinner.Model

	loading     bool
	width       int
	height      int
	token       string
	subType     string
	lastRefresh time.Time // debounce

	tokensToday TokenStats
	tokens7d    TokenStats
	tokensErr   error
}

// narrow returns true when the terminal is too tight for the full layout
func (m model) narrow() bool {
	return m.contentWidth() < 44
}

// contentWidth returns usable width inside the border
func (m model) contentWidth() int {
	if m.width <= 0 {
		return 50
	}
	pad := 6 // 2 border + 4 padding
	if m.narrow2() {
		pad = 4 // 2 border + 2 padding
	}
	return m.width - pad
}

// narrow2 is the raw width check (no contentWidth recursion)
func (m model) narrow2() bool {
	return m.width < 35
}

func (m model) labelWidth() int {
	if m.narrow() {
		return 6
	}
	return 16
}

func (m model) borderStyle() lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("99"))
	if m.narrow2() {
		return s.Padding(0, 1)
	}
	return s.Padding(1, 2)
}

func newModel(token, subType string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))

	barWidth := 30

	return model{
		sessionBar: newBar(barWidth),
		weeklyBar:  newBar(barWidth),
		opusBar:    newBar(barWidth),
		spinner:    s,
		loading:    true,
		token:      token,
		subType:    subType,
	}
}

func newBar(width int) progress.Model {
	p := progress.New(
		progress.WithScaledGradient("#76EEC6", "#FF6347"),
		progress.WithWidth(width),
		progress.WithoutPercentage(),
	)
	return p
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchCmd(m.token),
		fetchTokensCmd(),
		tickCmd(),
	)
}

func fetchCmd(token string) tea.Cmd {
	return func() tea.Msg {
		usage, err := fetchUsage(token)
		return usageFetchedMsg{usage: usage, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Minute, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchTokensCmd() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		today, err := scanTokens(startOfDay)
		if err != nil {
			return tokensFetchedMsg{err: err}
		}
		week, err := scanTokens(now.AddDate(0, 0, -7))
		if err != nil {
			return tokensFetchedMsg{today: today, err: err}
		}
		return tokensFetchedMsg{today: today, week: week}
	}
}

func (m *model) resizeBars() {
	cw := m.contentWidth()
	// bar = content - label - " " - percent(6)
	barWidth := cw - m.labelWidth() - 7
	barWidth = max(8, min(barWidth, 40))
	m.sessionBar.Width = barWidth
	m.weeklyBar.Width = barWidth
	m.opusBar.Width = barWidth
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if time.Since(m.lastRefresh) < 10*time.Second {
				return m, nil
			}
			m.loading = true
			m.lastRefresh = time.Now()
			return m, tea.Batch(m.spinner.Tick, fetchCmd(m.token), fetchTokensCmd())
		}

	case usageFetchedMsg:
		m.loading = false
		if msg.err != nil {
			if m.usage != nil {
				m.stale = true
				m.err = msg.err
			} else {
				m.err = msg.err
			}
			return m, nil
		}
		m.usage = msg.usage
		m.err = nil
		m.stale = false
		m.lastFetch = time.Now()

		var cmds []tea.Cmd
		if m.usage.FiveHour != nil {
			cmds = append(cmds, m.sessionBar.SetPercent(m.usage.FiveHour.Utilization/100))
		}
		if m.usage.SevenDay != nil {
			cmds = append(cmds, m.weeklyBar.SetPercent(m.usage.SevenDay.Utilization/100))
		}
		if m.usage.SevenDayOpus != nil {
			cmds = append(cmds, m.opusBar.SetPercent(m.usage.SevenDayOpus.Utilization/100))
		}
		return m, tea.Batch(cmds...)

	case tokensFetchedMsg:
		if msg.err == nil {
			m.tokensToday = msg.today
			m.tokens7d = msg.week
		}
		m.tokensErr = msg.err
		return m, nil

	case tickMsg:
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchCmd(m.token), fetchTokensCmd(), tickCmd())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeBars()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		var cmds []tea.Cmd

		pm, c := m.sessionBar.Update(msg)
		m.sessionBar = pm.(progress.Model)
		cmds = append(cmds, c)

		pm, c = m.weeklyBar.Update(msg)
		m.weeklyBar = pm.(progress.Model)
		cmds = append(cmds, c)

		pm, c = m.opusBar.Update(msg)
		m.opusBar = pm.(progress.Model)
		cmds = append(cmds, c)

		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	// title row
	title := titleStyle.Render("claude-usage")
	right := ""
	if m.loading {
		right = m.spinner.View()
	} else if m.stale {
		right = staleStyle.Render("stale")
	}
	titleRow := title
	if right != "" {
		titleRow += "  " + right
	}
	b.WriteString(titleRow + "\n\n")

	// error only (no data yet)
	if m.err != nil && m.usage == nil {
		b.WriteString(errorStyle.Render("  "+m.err.Error()) + "\n")
		return m.borderStyle().Render(b.String())
	}

	narrow := m.narrow()
	lw := m.labelWidth()
	resetIndent := lw

	if m.usage != nil {
		if m.usage.FiveHour != nil {
			label := "Session (5h)"
			if narrow {
				label = "5h"
			}
			b.WriteString(m.renderBar(label, m.sessionBar, m.usage.FiveHour, lw, resetIndent))
		}
		if m.usage.SevenDay != nil {
			label := "Weekly (7d)"
			if narrow {
				label = "7d"
			}
			b.WriteString(m.renderBar(label, m.weeklyBar, m.usage.SevenDay, lw, resetIndent))
		}
		if m.usage.SevenDayOpus != nil {
			label := "Opus (7d)"
			if narrow {
				label = "Opus"
			}
			b.WriteString(m.renderBar(label, m.opusBar, m.usage.SevenDayOpus, lw, resetIndent))
		}
	}

	// token counts
	if m.tokensToday.Total() > 0 || m.tokens7d.Total() > 0 {
		b.WriteString(m.renderTokenSection())
	}

	// stale error
	if m.stale && m.err != nil {
		b.WriteString(staleStyle.Render("  "+m.err.Error()) + "\n\n")
	}

	// footer
	footer := ""
	if m.subType != "" {
		footer += strings.ToUpper(m.subType[:1]) + m.subType[1:]
	}
	if !m.lastFetch.IsZero() {
		if footer != "" {
			footer += "  •  "
		}
		footer += "updated " + m.lastFetch.Format("15:04")
	}
	if footer != "" {
		b.WriteString(footerStyle.Render(footer))
	}

	return m.borderStyle().Render(b.String())
}

func (m model) renderBar(label string, bar progress.Model, bucket *UsageBucket, labelWidth, resetIndent int) string {
	pct := bucket.Utilization
	pctStr := percentStyle.Render(fmt.Sprintf("%.0f%%", pct))
	labelStr := lipgloss.NewStyle().Width(labelWidth).Foreground(labelColor).Render(label)
	line := labelStr + bar.View() + " " + pctStr + "\n"

	resetStr := ""
	if bucket.ResetsAt != nil {
		resetStr = formatReset(*bucket.ResetsAt)
	} else {
		resetStr = "—"
	}
	resetLine := lipgloss.NewStyle().Foreground(resetColor).MarginLeft(resetIndent).Render(resetStr) + "\n"

	return line + resetLine + "\n"
}

func (m model) renderTokenSection() string {
	var b strings.Builder
	narrow := m.narrow()

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	todayLabel := "Today"
	weekLabel := "Last 7 days"
	if narrow {
		todayLabel = "1d"
		weekLabel = "7d"
	}

	lw := m.labelWidth()

	renderRow := func(label string, stats TokenStats) string {
		labelStr := lipgloss.NewStyle().Width(lw).Foreground(labelColor).Render(label)
		in := valStyle.Render(formatTokenCount(stats.InputTokens))
		out := valStyle.Render(formatTokenCount(stats.OutputTokens))
		inLabel := dimStyle.Render(" in  ")
		outLabel := dimStyle.Render(" out")
		return labelStr + in + inLabel + out + outLabel + "\n"
	}

	if m.tokensToday.Total() > 0 {
		b.WriteString(renderRow(todayLabel, m.tokensToday))
	}
	if m.tokens7d.Total() > 0 {
		b.WriteString(renderRow(weekLabel, m.tokens7d))
	}
	b.WriteString("\n")

	return b.String()
}

func formatReset(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}

	until := time.Until(t)
	if until <= 0 {
		return "resetting..."
	}

	if until < time.Hour {
		return fmt.Sprintf("resets in %dm", int(math.Ceil(until.Minutes())))
	}
	if until < 24*time.Hour {
		h := int(until.Hours())
		m := int(until.Minutes()) % 60
		return fmt.Sprintf("resets in %dh %dm", h, m)
	}
	return "resets " + t.Local().Format("Mon Jan 2")
}
