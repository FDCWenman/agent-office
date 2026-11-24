// agent-office shows your running Claude Code sessions in the terminal.
//
//	agent-office        open the office (a Bubble Tea app)
//	agent-office hook   forward one Claude Code hook event to the office
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `usage:
  agent-office        open the office
  agent-office hook   forward a Claude Code hook event (reads the event JSON on stdin)`

func socketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".agent-office", "office.sock")
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook":
			// Always exit 0 with no output: a hook must never block or change Claude.
			runHook(os.Stdin, socketPath())
			return
		case "-h", "--help", "help":
			fmt.Println(usage)
			return
		default:
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
	}
	if err := runOffice(socketPath()); err != nil {
		fmt.Fprintln(os.Stderr, "agent-office:", err)
		os.Exit(1)
	}
}

func runOffice(path string) error {
	l, err := listen(path)
	if err != nil {
		return err
	}
	defer l.Close() // also removes the socket file

	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	go serve(l, func(e Event) { p.Send(eventMsg(e)) })
	_, err = p.Run()
	return err
}
