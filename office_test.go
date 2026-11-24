package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func ev(kind string, at time.Time, opts ...func(*Event)) Event {
	e := Event{Time: at, Kind: kind, SessionID: "abcd1234", Cwd: "/work/storefront"}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func tool(name, detail string) func(*Event) {
	return func(e *Event) { e.Tool, e.Detail = name, detail }
}

func TestApplyLifecycle(t *testing.T) {
	o := NewOffice()
	t0 := time.Now()
	steps := []struct {
		e         Event
		want      State
		wantAlert bool
	}{
		{ev("SessionStart", t0), StateIdle, false},
		{ev("UserPromptSubmit", t0), StateThinking, false},
		{ev("PreToolUse", t0, tool("Edit", "app/x.php")), StateTyping, false},
		{ev("PostToolUse", t0), StateThinking, false},
		{ev("PreToolUse", t0, tool("Bash", "git push")), StateRunning, false},
		{ev("PermissionRequest", t0, tool("Bash", "git push")), StateWaiting, true},
		// Same prompt reported again by Notification: no second alert.
		{ev("Notification", t0, func(e *Event) { e.NotificationType = "permission_prompt" }), StateWaiting, false},
		// A subagent's tool call must not hide the open permission prompt.
		{ev("PreToolUse", t0, tool("Read", "a.go")), StateWaiting, false},
		{ev("PostToolUse", t0), StateThinking, false},
		{ev("Stop", t0), StateDone, false},
		{ev("StopFailure", t0), StateFailed, true},
		{ev("SessionEnd", t0), StateEnded, false},
	}
	for i, s := range steps {
		alert := o.Apply(s.e)
		a := o.agents["abcd1234"]
		if a.State != s.want || alert != s.wantAlert {
			t.Fatalf("step %d (%s): state=%v alert=%v, want %v alert=%v", i, s.e.Kind, a.State, alert, s.want, s.wantAlert)
		}
	}
	if a := o.agents["abcd1234"]; a.Tool != "Bash" || a.Detail != "git push" {
		t.Errorf("waiting tool = %q %q, want the permission prompt's tool kept", a.Tool, a.Detail)
	}
	if len(o.agents["abcd1234"].Log) != len(steps)-2 { // PostToolUse is not logged
		t.Errorf("log has %d entries, want %d", len(o.agents["abcd1234"].Log), len(steps)-2)
	}
}

func TestSubagentsAndPrune(t *testing.T) {
	o := NewOffice()
	t0 := time.Now()
	o.Apply(ev("SubagentStart", t0))
	o.Apply(ev("SubagentStart", t0))
	o.Apply(ev("SubagentStop", t0))
	if n := o.agents["abcd1234"].Subagents; n != 1 {
		t.Fatalf("subagents = %d, want 1", n)
	}
	o.Apply(ev("SessionEnd", t0))
	o.Prune(t0.Add(endedTTL - time.Second))
	if len(o.order) != 1 {
		t.Fatal("ended agent pruned too early")
	}
	o.Prune(t0.Add(endedTTL + time.Second))
	if len(o.order) != 0 || len(o.agents) != 0 {
		t.Fatal("ended agent not pruned")
	}
	if o.Apply(ev("SessionEnd", t0)); len(o.order) != 0 {
		t.Fatal("SessionEnd for an unknown session must not add it")
	}
}

func TestNameAddsIDOnlyWhenFoldersClash(t *testing.T) {
	o := NewOffice()
	t0 := time.Now()
	o.Apply(ev("SessionStart", t0))
	if got := o.Name(o.agents["abcd1234"]); got != "storefront" {
		t.Fatalf("name = %q", got)
	}
	o.Apply(ev("SessionStart", t0, func(e *Event) { e.SessionID = "ffff9999" }))
	if got := o.Name(o.agents["abcd1234"]); got != "storefront #abcd" {
		t.Fatalf("name = %q", got)
	}
}

func TestEventFromHook(t *testing.T) {
	in := hookInput{
		SessionID: "s1", Cwd: "/work/repo", HookEventName: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "/work/repo/app/x.php", "old_string": "secret"},
	}
	if e := eventFromHook(in, time.Now()); e.Detail != "app/x.php" || e.Tool != "Edit" {
		t.Errorf("edit detail = %q", e.Detail)
	}
	in.ToolName, in.ToolInput = "Bash", map[string]any{"command": "echo a\necho b", "description": "two echos"}
	if e := eventFromHook(in, time.Now()); e.Detail != "echo a …" {
		t.Errorf("bash detail = %q", e.Detail)
	}
	in.ToolInput = map[string]any{"command": strings.Repeat("x", 500)}
	if e := eventFromHook(in, time.Now()); len([]rune(e.Detail)) != detailLimit {
		t.Errorf("detail not truncated: %d runes", len([]rune(e.Detail)))
	}
}

func TestHookReachesOffice(t *testing.T) {
	dir, err := os.MkdirTemp("", "ao") // short path: Unix socket paths max out at 104 bytes on macOS
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "office.sock")

	l, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := listen(path); err != errAlreadyRunning {
		t.Fatalf("second listen: err = %v, want errAlreadyRunning", err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, err %v", fi.Mode().Perm(), err)
	}

	got := make(chan Event, 1)
	go serve(l, func(e Event) { got <- e })
	runHook(strings.NewReader(`{"session_id":"s1","cwd":"/w","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/w/a.go"}}`), path)

	select {
	case e := <-got:
		if e.Kind != "PreToolUse" || e.Tool != "Read" || e.Detail != "a.go" || e.SessionID != "s1" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event never arrived")
	}
}

func TestHookIsSilentWhenOfficeIsClosed(t *testing.T) {
	start := time.Now()
	runHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`), "/nonexistent/office.sock")
	runHook(strings.NewReader(`not json`), "/nonexistent/office.sock")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("hook took %v with no office running", d)
	}
}

func TestView(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 30
	if !strings.Contains(m.View(), "No agents yet.") {
		t.Fatal("empty office should say so")
	}
	t0 := time.Now()
	m.now = t0
	m.office.Apply(ev("UserPromptSubmit", t0))
	m.office.Apply(ev("PreToolUse", t0, tool("Bash", "git push origin feature-1")))
	m.office.Apply(ev("PermissionRequest", t0, tool("Bash", "git push origin feature-1")))

	out := m.View()
	for _, want := range []string{"storefront", "needs permission", "Bash: git push origin feature-1", "1 needs you", "you sent a prompt"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Errorf("view is %d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("line %d is %d wide, max %d", i, w, m.width)
		}
	}
}

func TestOfficeScene(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40
	lines := m.officeLines(120, 100)
	if !strings.Contains(strings.Join(lines, ""), "waiting for agents") {
		t.Fatal("empty office should show its placeholder plate")
	}

	t0 := time.Now()
	m.now = t0
	m.office.Apply(ev("PreToolUse", t0, tool("Edit", "ui.go")))
	for i := 0; i < 5; i++ { // second row of desks: 6 agents, 4 per row at 120 wide
		m.office.Apply(ev("UserPromptSubmit", t0, func(e *Event) { e.SessionID = "s" + strconv.Itoa(i); e.Cwd = "/w/p" + strconv.Itoa(i) }))
	}
	want := sceneRows(6, 120)
	if want != (sceneTop+2*stationH)/2 {
		t.Fatalf("sceneRows = %d, want two rows of desks", want)
	}
	lines = m.officeLines(120, 100)
	if len(lines) != want {
		t.Fatalf("office is %d rows, want %d", len(lines), want)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 120 {
			t.Fatalf("office row %d is %d wide, want 120", i, w)
		}
	}
	plain := ansi.Strip(strings.Join(lines, "\n"))
	for _, name := range []string{"▶ ● storefront", "p4"} {
		if !strings.Contains(plain, name) {
			t.Errorf("office missing plate %q", name)
		}
	}
	if got := m.officeLines(120, 10); len(got) != 10 {
		t.Errorf("cropped office is %d rows, want 10", len(got))
	}

	// The typing agent animates: consecutive frames differ.
	m.office = NewOffice()
	m.office.Apply(ev("PreToolUse", t0, tool("Edit", "ui.go")))
	m.frame = 0
	a := strings.Join(m.officeLines(120, 100), "\n")
	m.frame = 1
	if b := strings.Join(m.officeLines(120, 100), "\n"); a == b {
		t.Error("typing agent did not animate between frames")
	}
}

func TestViewHidesOfficeWhenShort(t *testing.T) {
	m := newModel()
	m.office.Apply(ev("UserPromptSubmit", time.Now()))
	for _, h := range []int{20, 30, 45} {
		m.width, m.height = 100, h
		out := m.View()
		if n := len(strings.Split(out, "\n")); n != h {
			t.Errorf("height %d: view is %d lines", h, n)
		}
		hasOffice := strings.Contains(out, "▀")
		if wantOffice := h-2-minPanelH >= minOfficeRows; hasOffice != wantOffice {
			t.Errorf("height %d: office shown = %v, want %v", h, hasOffice, wantOffice)
		}
	}
}
