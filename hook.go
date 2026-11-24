package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Event is what the hook sends to the office: a small summary of one Claude
// Code hook event. Prompt text and file contents are never forwarded.
type Event struct {
	Time             time.Time `json:"time"`
	Kind             string    `json:"kind"` // hook_event_name, e.g. PreToolUse
	SessionID        string    `json:"session_id"`
	Cwd              string    `json:"cwd,omitempty"`
	Tool             string    `json:"tool,omitempty"`
	Detail           string    `json:"detail,omitempty"`
	NotificationType string    `json:"notification_type,omitempty"`
	AgentType        string    `json:"agent_type,omitempty"`
	Source           string    `json:"source,omitempty"`
}

// hookInput is the part of Claude Code's hook JSON (on stdin) that we use.
type hookInput struct {
	SessionID        string         `json:"session_id"`
	Cwd              string         `json:"cwd"`
	HookEventName    string         `json:"hook_event_name"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
	Message          string         `json:"message"`
	NotificationType string         `json:"notification_type"`
	AgentType        string         `json:"agent_type"`
	Source           string         `json:"source"`
}

const (
	sendTimeout = 150 * time.Millisecond
	detailLimit = 120
)

func runHook(r io.Reader, path string) {
	var in hookInput
	if err := json.NewDecoder(r).Decode(&in); err != nil || in.SessionID == "" || in.HookEventName == "" {
		return
	}
	b, err := json.Marshal(eventFromHook(in, time.Now()))
	if err != nil {
		return
	}
	c, err := net.DialTimeout("unix", path, sendTimeout)
	if err != nil {
		return // office not running
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(sendTimeout))
	_, _ = c.Write(append(b, '\n'))
}

func eventFromHook(in hookInput, now time.Time) Event {
	e := Event{
		Time:             now,
		Kind:             in.HookEventName,
		SessionID:        in.SessionID,
		Cwd:              in.Cwd,
		Tool:             in.ToolName,
		NotificationType: in.NotificationType,
		AgentType:        in.AgentType,
		Source:           in.Source,
	}
	if in.HookEventName == "Notification" {
		e.Detail = truncate(in.Message, detailLimit)
	} else {
		e.Detail = truncate(toolDetail(in.ToolInput, in.Cwd), detailLimit)
	}
	return e
}

// toolDetail picks the one field of a tool call worth showing.
func toolDetail(input map[string]any, cwd string) string {
	str := func(k string) string { s, _ := input[k].(string); return s }
	switch {
	case str("file_path") != "":
		return displayPath(str("file_path"), cwd)
	case str("notebook_path") != "":
		return displayPath(str("notebook_path"), cwd)
	case str("command") != "":
		return firstLine(str("command"))
	case str("pattern") != "":
		return str("pattern")
	case str("url") != "":
		return str("url")
	case str("query") != "":
		return str("query")
	case str("description") != "":
		return str("description")
	}
	return ""
}

// displayPath shows p relative to cwd when it is inside it, else with ~ for home.
func displayPath(p, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return tildePath(p)
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
