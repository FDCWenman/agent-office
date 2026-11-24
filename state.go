package main

import (
	"path/filepath"
	"time"
)

type State int

const (
	StateIdle State = iota
	StateThinking
	StateTyping
	StateReading
	StateRunning
	StateDelegating
	StateWorking
	StateWaiting
	StateDone
	StateFailed
	StateEnded
)

var stateLabels = map[State]string{
	StateIdle:       "idle",
	StateThinking:   "thinking",
	StateTyping:     "typing",
	StateReading:    "reading",
	StateRunning:    "running",
	StateDelegating: "delegating",
	StateWorking:    "working",
	StateWaiting:    "needs permission",
	StateDone:       "your turn",
	StateFailed:     "error",
	StateEnded:      "ended",
}

func (s State) String() string { return stateLabels[s] }

// Active reports whether Claude is busy on its own.
func (s State) Active() bool { return s >= StateThinking && s <= StateWorking }

const (
	logLimit = 100
	endedTTL = 10 * time.Minute
)

type Agent struct {
	SessionID string
	Cwd       string
	State     State
	Tool      string // current or last tool
	Detail    string
	Since     time.Time // when State was entered
	LastSeen  time.Time
	Subagents int
	Log       []Event
}

// set changes the state and reports whether it changed.
func (a *Agent) set(s State, t time.Time) bool {
	if a.State == s {
		return false
	}
	a.State, a.Since = s, t
	return true
}

// Office holds every known session, in the order first seen.
type Office struct {
	agents map[string]*Agent
	order  []string
}

func NewOffice() *Office { return &Office{agents: map[string]*Agent{}} }

func (o *Office) Agents() []*Agent {
	out := make([]*Agent, 0, len(o.order))
	for _, id := range o.order {
		out = append(out, o.agents[id])
	}
	return out
}

// Apply updates the office with one event and reports whether the user
// should be alerted (an agent started waiting on them or failed).
func (o *Office) Apply(e Event) (alert bool) {
	a, ok := o.agents[e.SessionID]
	if !ok {
		if e.Kind == "SessionEnd" {
			return false
		}
		a = &Agent{SessionID: e.SessionID, Cwd: e.Cwd, State: StateIdle, Since: e.Time}
		o.agents[e.SessionID] = a
		o.order = append(o.order, e.SessionID)
	}
	if a.Cwd == "" {
		a.Cwd = e.Cwd
	}
	a.LastSeen = e.Time
	if e.Kind != "PostToolUse" { // every tool call already logs its PreToolUse
		a.Log = append(a.Log, e)
		if len(a.Log) > logLimit {
			a.Log = a.Log[len(a.Log)-logLimit:]
		}
	}

	switch e.Kind {
	case "SessionStart":
		if e.Source != "compact" {
			a.set(StateIdle, e.Time)
			a.Tool, a.Detail = "", ""
		}
	case "UserPromptSubmit":
		a.set(StateThinking, e.Time)
		a.Tool, a.Detail = "", ""
	case "PreToolUse":
		// Subagents keep calling tools while a permission prompt is open;
		// never let them hide that the user is needed.
		if a.State != StateWaiting {
			a.set(toolState(e.Tool), e.Time)
			a.Tool, a.Detail = e.Tool, e.Detail
		}
	case "PostToolUse", "PostToolUseFailure":
		a.set(StateThinking, e.Time)
	case "PermissionRequest":
		alert = a.set(StateWaiting, e.Time)
		if e.Tool != "" {
			a.Tool, a.Detail = e.Tool, e.Detail
		}
	case "Notification":
		switch e.NotificationType {
		case "permission_prompt":
			alert = a.set(StateWaiting, e.Time)
		case "idle_prompt":
			a.set(StateDone, e.Time)
		}
	case "Stop":
		a.set(StateDone, e.Time)
	case "StopFailure":
		alert = a.set(StateFailed, e.Time)
	case "SubagentStart":
		a.Subagents++
	case "SubagentStop":
		if a.Subagents > 0 {
			a.Subagents--
		}
	case "SessionEnd":
		a.set(StateEnded, e.Time)
		a.Subagents = 0
	}
	return alert
}

func toolState(tool string) State {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return StateTyping
	case "Read", "Grep", "Glob", "LS", "WebFetch", "WebSearch", "ToolSearch":
		return StateReading
	case "Bash", "BashOutput", "KillShell", "Monitor", "TaskStop":
		return StateRunning
	case "Task", "Agent":
		return StateDelegating
	}
	return StateWorking
}

// Name is the project folder, plus a short session id when two agents share it.
func (o *Office) Name(a *Agent) string {
	base := "?"
	if a.Cwd != "" {
		base = filepath.Base(a.Cwd)
	}
	for _, id := range o.order {
		if b := o.agents[id]; b != a && b.Cwd != "" && filepath.Base(b.Cwd) == base {
			return base + " #" + shortID(a.SessionID)
		}
	}
	return base
}

func shortID(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

func (o *Office) Waiting() int {
	n := 0
	for _, a := range o.agents {
		if a.State == StateWaiting {
			n++
		}
	}
	return n
}

func (o *Office) Remove(id string) {
	delete(o.agents, id)
	for i, v := range o.order {
		if v == id {
			o.order = append(o.order[:i], o.order[i+1:]...)
			return
		}
	}
}

// ClearEnded removes every ended session.
func (o *Office) ClearEnded() { o.removeEnded(func(*Agent) bool { return true }) }

// Prune removes sessions that ended more than endedTTL ago.
func (o *Office) Prune(now time.Time) {
	o.removeEnded(func(a *Agent) bool { return now.Sub(a.Since) > endedTTL })
}

func (o *Office) removeEnded(match func(*Agent) bool) {
	for _, a := range o.Agents() {
		if a.State == StateEnded && match(a) {
			o.Remove(a.SessionID)
		}
	}
}
