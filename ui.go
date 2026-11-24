package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type (
	eventMsg Event
	tickMsg  time.Time
)

const frameInterval = 200 * time.Millisecond

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// minPanelH keeps at least two agents visible in the list under the office.
const minPanelH = 6

func stateColor(s State) lipgloss.Color { return lipgloss.Color(stateRGB[s].hex()) }

var (
	styleDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleBold  = lipgloss.NewStyle().Bold(true)
	styleAlert = lipgloss.NewStyle().Bold(true).Foreground(stateColor(StateWaiting))
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).Padding(0, 1)
	stylePanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

type model struct {
	office        *Office
	selected      int
	width, height int
	frame         int
	now           time.Time
	ring          func()
}

func newModel() model {
	return model{
		office: NewOffice(),
		now:    time.Now(),
		ring:   func() { fmt.Fprint(os.Stderr, "\a") }, // terminal bell
	}
}

func tick() tea.Cmd {
	return tea.Tick(frameInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return tea.Batch(tick(), m.titleCmd()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		if m.office.Apply(Event(msg)) {
			m.ring()
		}
		return m, m.titleCmd()
	case tickMsg:
		m.now = time.Time(msg)
		m.frame++
		m.office.Prune(m.now)
		m.clamp()
		return m, tick()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			m.selected++
		case "x":
			if a := m.current(); a != nil {
				m.office.Remove(a.SessionID)
			}
		case "c":
			m.office.ClearEnded()
		}
		m.clamp()
	}
	return m, nil
}

func (m *model) clamp() {
	if n := len(m.office.order); m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m model) current() *Agent {
	if m.selected < len(m.office.order) {
		return m.office.agents[m.office.order[m.selected]]
	}
	return nil
}

func (m model) titleCmd() tea.Cmd {
	if n := m.office.Waiting(); n > 0 {
		return tea.SetWindowTitle(fmt.Sprintf("agent office · %d need%s you", n, pluralVerb(n)))
	}
	return tea.SetWindowTitle("agent office")
}

func (m model) View() string {
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		w, h = 100, 30 // before the first WindowSizeMsg
	}
	header := m.header(w)
	footer := styleDim.Render(" ↑/↓ select · x remove · c clear ended · q quit")
	avail := h - 2
	if avail < 4 {
		return header
	}

	// The office goes on top when there is room for at least one row of desks.
	officeRows := 0
	if room := avail - minPanelH; room >= minOfficeRows {
		officeRows = min(sceneRows(len(m.office.order), w), room)
	}
	panelH := avail - officeRows

	listW := w
	if w >= 90 {
		listW = w * 45 / 100
	}
	body := panel(m.listLines(listW-4, panelH-2), listW, panelH)
	if listW < w {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, panel(m.detailLines(panelH-2), w-listW, panelH))
	}
	parts := []string{header}
	if officeRows > 0 {
		parts = append(parts, strings.Join(m.officeLines(w, officeRows), "\n"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(parts, body, footer)...)
}

func (m model) header(w int) string {
	agents := m.office.Agents()
	summary := fmt.Sprintf(" %d agent%s", len(agents), plural(len(agents)))
	if n := m.office.Waiting(); n > 0 {
		summary += " · " + styleAlert.Render(fmt.Sprintf("%d need%s you", n, pluralVerb(n)))
	}
	return spread(styleTitle.Render("agent office")+summary, styleDim.Render(m.now.Format("15:04:05"))+" ", w)
}

func (m model) listLines(w, h int) []string {
	agents := m.office.Agents()
	if len(agents) == 0 {
		return []string{
			styleBold.Render("No agents yet."),
			"",
			styleDim.Render("Claude Code sessions show up here on"),
			styleDim.Render("their next event (a prompt or tool call)."),
		}
	}
	visible := h / 2 // two lines per agent
	start := 0
	if visible > 0 && m.selected >= visible {
		start = m.selected - visible + 1
	}
	var lines []string
	for i := start; i < len(agents) && len(lines)+2 <= h; i++ {
		a := agents[i]
		color := lipgloss.NewStyle().Foreground(stateColor(a.State))
		marker, name := "  ", m.office.Name(a)
		if i == m.selected {
			marker, name = styleBold.Render("▶ "), styleBold.Render(name)
		}
		left := marker + color.Render(m.icon(a.State)) + " " + name
		right := color.Render(a.State.String()) + styleDim.Render("  "+age(m.now.Sub(a.Since)))
		lines = append(lines, spread(left, right, w))

		sub := ""
		if a.Tool != "" {
			sub = a.Tool
			if a.Detail != "" {
				sub += ": " + a.Detail
			}
		}
		if a.Subagents > 0 {
			sub = strings.TrimSpace(sub + fmt.Sprintf("  +%d subagent%s", a.Subagents, plural(a.Subagents)))
		}
		lines = append(lines, styleDim.Render("    "+sub))
	}
	return lines
}

func (m model) detailLines(h int) []string {
	a := m.current()
	if a == nil {
		return nil
	}
	color := lipgloss.NewStyle().Foreground(stateColor(a.State))
	status := color.Render(a.State.String()) + styleDim.Render(" for "+age(m.now.Sub(a.Since)))
	if a.Subagents > 0 {
		status += styleDim.Render(fmt.Sprintf(" · %d subagent%s", a.Subagents, plural(a.Subagents)))
	}
	lines := []string{
		styleBold.Render(m.office.Name(a)) + "  " + styleDim.Render(tildePath(a.Cwd)),
		styleDim.Render("session "+shortID(a.SessionID)+" · ") + status,
	}
	log := a.Log // newest entries that fit
	if room := max(h-len(lines), 0); len(log) > room {
		log = log[len(log)-room:]
	}
	for _, e := range log {
		lines = append(lines, styleDim.Render(e.Time.Local().Format("15:04:05"))+"  "+logText(e))
	}
	return lines
}

func logText(e Event) string {
	with := func(s, extra string) string {
		if extra != "" {
			return s + " " + extra
		}
		return s
	}
	switch e.Kind {
	case "SessionStart":
		return with("session started", e.Source)
	case "SessionEnd":
		return "session ended"
	case "UserPromptSubmit":
		return "you sent a prompt"
	case "PreToolUse":
		return lipgloss.NewStyle().Foreground(stateColor(toolState(e.Tool))).Render(e.Tool) + "  " + e.Detail
	case "PostToolUseFailure":
		return styleAlert.Render("✗ "+e.Tool+" failed") + "  " + e.Detail
	case "PermissionRequest":
		return styleAlert.Render("! permission: "+e.Tool) + "  " + e.Detail
	case "Notification":
		return with("!", e.Detail)
	case "Stop":
		return "✓ finished its turn"
	case "StopFailure":
		return styleAlert.Render("✗ stopped with an error")
	case "SubagentStart":
		return with("↳ subagent started", e.AgentType)
	case "SubagentStop":
		return with("↳ subagent finished", e.AgentType)
	}
	return e.Kind
}

func (m model) icon(s State) string {
	switch {
	case s.Active():
		return spinner[m.frame%len(spinner)]
	case s == StateWaiting:
		if (m.frame/3)%2 == 0 { // blink
			return "●"
		}
		return "○"
	case s == StateDone:
		return "✓"
	case s == StateFailed:
		return "✗"
	case s == StateEnded:
		return "–"
	}
	return "·"
}

// panel draws lines in a rounded box exactly w×h cells, cutting what doesn't fit.
func panel(lines []string, w, h int) string {
	innerW, innerH := w-4, h-2
	if innerW < 1 || innerH < 1 {
		return ""
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Truncate(l, innerW, "…")
	}
	return stylePanel.Width(w - 2).Height(innerH).Render(strings.Join(out, "\n"))
}

// spread puts left and right on one line of width w.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left+" "+right, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralVerb(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}
