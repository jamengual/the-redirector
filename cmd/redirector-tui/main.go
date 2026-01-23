package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Padding(0, 1)

	statBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			Width(20)

	statLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	statValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	errorValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))

	statusOKStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))

	statusRedirectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("226"))

	statusErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)
)

// RequestRecord matches the stats package structure
type RequestRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	Path        string    `json:"path"`
	Host        string    `json:"host"`
	Status      int       `json:"status"`
	RuleID      string    `json:"rule_id"`
	Destination string    `json:"destination"`
	LatencyUs   int64     `json:"latency_us"`
	ClientIP    string    `json:"client_ip"`
}

// Summary matches the stats package structure
type Summary struct {
	UptimeSeconds  float64           `json:"uptime_seconds"`
	TotalRequests  int64             `json:"total_requests"`
	TotalErrors    int64             `json:"total_errors"`
	RequestsPerSec float64           `json:"requests_per_second"`
	ErrorRate      float64           `json:"error_rate"`
	StatusCounts   map[string]int64  `json:"status_counts"`
	LatencyBuckets map[string]int64  `json:"latency_buckets"`
	TopRules       []RuleStats       `json:"top_rules"`
}

// RuleStats matches the stats package structure
type RuleStats struct {
	ID           string  `json:"id"`
	Hits         int64   `json:"hits"`
	TotalLatency int64   `json:"total_latency_us"`
	AvgLatency   float64 `json:"avg_latency_us"`
}

// SortField defines what column to sort by
type SortField int

const (
	SortByTime SortField = iota
	SortByStatus
	SortByLatency
	SortByPath
	SortByRule
)

// KeyMap defines keyboard shortcuts
type KeyMap struct {
	Quit       key.Binding
	Refresh    key.Binding
	SortTime   key.Binding
	SortStatus key.Binding
	SortLatency key.Binding
	SortPath   key.Binding
	SortRule   key.Binding
	TogglePause key.Binding
	Filter     key.Binding
	ClearFilter key.Binding
	Help       key.Binding
	Up         key.Binding
	Down       key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		SortTime:   key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "sort by time")),
		SortStatus: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "sort by status")),
		SortLatency: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "sort by latency")),
		SortPath:   key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "sort by path")),
		SortRule:   key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "sort by rule")),
		TogglePause: key.NewBinding(key.WithKeys("p", " "), key.WithHelp("p/space", "pause")),
		Filter:     key.NewBinding(key.WithKeys("f", "/"), key.WithHelp("f", "filter")),
		ClearFilter: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear filter")),
		Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	}
}

// Model is the bubbletea model
type Model struct {
	baseURL     string
	requests    []RequestRecord
	summary     Summary
	table       table.Model
	keys        KeyMap
	help        help.Model
	sortField   SortField
	sortReverse bool
	paused      bool
	filter      string
	filterMode  bool
	err         error
	width       int
	height      int
	lastUpdate  time.Time
}

// Messages
type tickMsg time.Time
type dataMsg struct {
	requests []RequestRecord
	summary  Summary
}
type errMsg error

func initialModel(baseURL string) Model {
	columns := []table.Column{
		{Title: "Time", Width: 12},
		{Title: "Status", Width: 6},
		{Title: "Latency", Width: 10},
		{Title: "Path", Width: 30},
		{Title: "Rule", Width: 20},
		{Title: "Destination", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(20),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return Model{
		baseURL:   baseURL,
		table:     t,
		keys:      defaultKeyMap(),
		help:      help.New(),
		sortField: SortByTime,
		width:     120,
		height:    40,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchData(m.baseURL),
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchData(baseURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 5 * time.Second}

		// Fetch summary
		summaryResp, err := client.Get(baseURL + "/stats")
		if err != nil {
			return errMsg(err)
		}
		defer summaryResp.Body.Close()

		var summary Summary
		if err := json.NewDecoder(summaryResp.Body).Decode(&summary); err != nil {
			return errMsg(err)
		}

		// Fetch recent requests
		requestsResp, err := client.Get(baseURL + "/stats/live?limit=100")
		if err != nil {
			return errMsg(err)
		}
		defer requestsResp.Body.Close()

		var requests []RequestRecord
		if err := json.NewDecoder(requestsResp.Body).Decode(&requests); err != nil {
			return errMsg(err)
		}

		return dataMsg{requests: requests, summary: summary}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - 4)
		m.table.SetHeight(msg.Height - 15)
		m.help.Width = msg.Width

	case tickMsg:
		if !m.paused {
			cmds = append(cmds, fetchData(m.baseURL))
		}
		cmds = append(cmds, tickCmd())

	case dataMsg:
		m.requests = msg.requests
		m.summary = msg.summary
		m.lastUpdate = time.Now()
		m.err = nil
		m.updateTable()

	case errMsg:
		m.err = msg

	case tea.KeyMsg:
		if m.filterMode {
			switch msg.String() {
			case "enter", "esc":
				m.filterMode = false
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.filter += msg.String()
				}
			}
			m.updateTable()
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			return m, fetchData(m.baseURL)
		case key.Matches(msg, m.keys.TogglePause):
			m.paused = !m.paused
		case key.Matches(msg, m.keys.SortTime):
			m.setSortField(SortByTime)
		case key.Matches(msg, m.keys.SortStatus):
			m.setSortField(SortByStatus)
		case key.Matches(msg, m.keys.SortLatency):
			m.setSortField(SortByLatency)
		case key.Matches(msg, m.keys.SortPath):
			m.setSortField(SortByPath)
		case key.Matches(msg, m.keys.SortRule):
			m.setSortField(SortByRule)
		case key.Matches(msg, m.keys.Filter):
			m.filterMode = true
		case key.Matches(msg, m.keys.ClearFilter):
			m.filter = ""
			m.updateTable()
		}
	}

	m.table, cmd = m.table.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) setSortField(field SortField) {
	if m.sortField == field {
		m.sortReverse = !m.sortReverse
	} else {
		m.sortField = field
		m.sortReverse = false
	}
	m.updateTable()
}

func (m *Model) updateTable() {
	// Filter requests
	filtered := m.requests
	if m.filter != "" {
		filtered = nil
		for _, r := range m.requests {
			if strings.Contains(strings.ToLower(r.Path), strings.ToLower(m.filter)) ||
				strings.Contains(strings.ToLower(r.RuleID), strings.ToLower(m.filter)) ||
				strings.Contains(strings.ToLower(r.Destination), strings.ToLower(m.filter)) {
				filtered = append(filtered, r)
			}
		}
	}

	// Sort requests
	sort.Slice(filtered, func(i, j int) bool {
		var less bool
		switch m.sortField {
		case SortByTime:
			less = filtered[i].Timestamp.After(filtered[j].Timestamp)
		case SortByStatus:
			less = filtered[i].Status < filtered[j].Status
		case SortByLatency:
			less = filtered[i].LatencyUs < filtered[j].LatencyUs
		case SortByPath:
			less = filtered[i].Path < filtered[j].Path
		case SortByRule:
			less = filtered[i].RuleID < filtered[j].RuleID
		}
		if m.sortReverse {
			return !less
		}
		return less
	})

	// Build table rows
	rows := make([]table.Row, len(filtered))
	for i, r := range filtered {
		latency := formatLatency(r.LatencyUs)
		rows[i] = table.Row{
			r.Timestamp.Format("15:04:05.000"),
			fmt.Sprintf("%d", r.Status),
			latency,
			truncate(r.Path, 30),
			truncate(r.RuleID, 20),
			truncate(r.Destination, 30),
		}
	}
	m.table.SetRows(rows)
}

func formatLatency(us int64) string {
	if us < 1000 {
		return fmt.Sprintf("%dµs", us)
	}
	if us < 1000000 {
		return fmt.Sprintf("%.2fms", float64(us)/1000)
	}
	return fmt.Sprintf("%.2fs", float64(us)/1000000)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title
	title := titleStyle.Render("⚡ The Redirector - Live Monitor")
	b.WriteString(title)
	b.WriteString("\n")

	// Stats bar
	b.WriteString(m.renderStatsBar())
	b.WriteString("\n\n")

	// Error display
	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	// Filter display
	if m.filterMode {
		b.WriteString(fmt.Sprintf("Filter: %s▌\n", m.filter))
	} else if m.filter != "" {
		b.WriteString(fmt.Sprintf("Filter: %s (press 'c' to clear)\n", m.filter))
	}

	// Table header
	sortIndicator := m.getSortIndicator()
	b.WriteString(headerStyle.Render(fmt.Sprintf("Recent Requests (%d) %s", len(m.requests), sortIndicator)))
	b.WriteString("\n")

	// Table
	b.WriteString(m.table.View())
	b.WriteString("\n")

	// Footer
	status := "▶ Live"
	if m.paused {
		status = "⏸ Paused"
	}
	footer := footerStyle.Render(fmt.Sprintf("%s | Last update: %s | Press ? for help",
		status, m.lastUpdate.Format("15:04:05")))
	b.WriteString(footer)
	b.WriteString("\n")

	// Help
	b.WriteString(helpStyle.Render(m.help.ShortHelpView([]key.Binding{
		m.keys.Quit, m.keys.TogglePause, m.keys.Filter, m.keys.Refresh,
	})))

	return b.String()
}

func (m Model) renderStatsBar() string {
	// Uptime
	uptime := statBoxStyle.Render(
		statLabelStyle.Render("Uptime") + "\n" +
			statValueStyle.Render(formatDuration(m.summary.UptimeSeconds)),
	)

	// Total requests
	requests := statBoxStyle.Render(
		statLabelStyle.Render("Total Requests") + "\n" +
			statValueStyle.Render(formatNumber(m.summary.TotalRequests)),
	)

	// Requests/sec
	rps := statBoxStyle.Render(
		statLabelStyle.Render("Req/sec") + "\n" +
			statValueStyle.Render(fmt.Sprintf("%.1f", m.summary.RequestsPerSec)),
	)

	// Errors
	errStyle := statValueStyle
	if m.summary.TotalErrors > 0 {
		errStyle = errorValueStyle
	}
	errors := statBoxStyle.Render(
		statLabelStyle.Render("Errors") + "\n" +
			errStyle.Render(formatNumber(m.summary.TotalErrors)),
	)

	// Error rate
	errRateStyle := statValueStyle
	if m.summary.ErrorRate > 0.01 {
		errRateStyle = errorValueStyle
	}
	errRate := statBoxStyle.Render(
		statLabelStyle.Render("Error Rate") + "\n" +
			errRateStyle.Render(fmt.Sprintf("%.2f%%", m.summary.ErrorRate*100)),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top, uptime, requests, rps, errors, errRate)
}

func (m Model) getSortIndicator() string {
	fields := []string{"time", "status", "latency", "path", "rule"}
	arrow := "↓"
	if m.sortReverse {
		arrow = "↑"
	}
	return fmt.Sprintf("[sorted by %s %s]", fields[m.sortField], arrow)
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

func main() {
	baseURL := flag.String("url", "http://localhost:8081", "Management API base URL")
	flag.Parse()

	p := tea.NewProgram(
		initialModel(*baseURL),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
