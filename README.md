# agent office

A terminal view of your running Claude Code sessions (Go + Bubble Tea).
Each session is a pixel-art character at a desk in a small office, drawn with
half-block characters (`▀`, two pixels per cell) in truecolor:

- typing hands and scrolling code for edits, a page for reads, terminal output
  for commands, a "..." bubble while thinking
- tiny helpers on the desk for running subagents
- a flashing "!" bubble — plus the terminal bell and window title — when an
  agent needs your permission; a check when it has finished its turn

Under the office, a list and the selected agent's recent activity. The office
hides itself when the terminal is too short (under ~27 rows); widen the pane
for more desks per row (one per 28 columns).

## Use

```sh
agent-office          # open it, e.g. in an iTerm2 split pane
```

Keys: `↑/↓` (or `j/k`) select · `x` remove the selected agent · `c` clear ended
sessions · `q` quit.

Sessions appear on their next hook event (a prompt or a tool call); sessions
that were already running before the office opened show up once they do
something.

## How it works

Claude Code runs `agent-office hook` on each hook event (registered in
`~/.claude/settings.json`). The hook reads the event JSON, keeps only a short
summary (event, tool, file or command — never prompt text or file contents),
and sends it to the office over a Unix socket at `~/.agent-office/office.sock`
(mode 0600). If the office is not running the hook exits at once, silently, with
status 0, so it never slows down or changes Claude.

## Install

Needs Go 1.24+.

```sh
git clone https://github.com/FDCWenman/agent-office.git
cd agent-office
go test ./...
go build -o ~/.local/bin/agent-office .    # any directory on your PATH
```

Then register the hook in `~/.claude/settings.json` (back the file up first).
This adds one `agent-office hook` entry per event and leaves your other hooks
alone; running it twice adds nothing:

```sh
S=~/.claude/settings.json
cp "$S" "$S.bak"
jq --arg cmd "$HOME/.local/bin/agent-office hook" '
  reduce ("SessionStart","SessionEnd","UserPromptSubmit","PreToolUse","PostToolUse",
          "PostToolUseFailure","PermissionRequest","Notification","Stop","StopFailure",
          "SubagentStart","SubagentStop") as $ev (.;
    if ([.hooks[$ev][]?.hooks[]?.command] | index($cmd)) then .
    else .hooks[$ev] = ((.hooks[$ev] // []) + [{"matcher": "", "hooks": [{"type": "command", "command": $cmd, "timeout": 5}]}])
    end)' "$S" > "$S.tmp" && mv "$S.tmp" "$S"
```

## Remove

```sh
S=~/.claude/settings.json
jq --arg cmd "$HOME/.local/bin/agent-office hook" '
  .hooks |= (map_values(map(select(([.hooks[]?.command] | index($cmd)) | not)))
             | with_entries(select(.value | length > 0)))' "$S" > "$S.tmp" && mv "$S.tmp" "$S"
rm ~/.local/bin/agent-office
```

## Notes

- Needs a truecolor terminal (iTerm2, Ghostty, WezTerm, kitty).
- All sprites are original, drawn in `scene.go`.
- Known limit: when subagents run in parallel, their tool calls share the parent
  session's state, so the shown activity can flicker between them. A pending
  permission prompt is never hidden by them.
