package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

var errAlreadyRunning = errors.New("another agent-office is already running")

// listen opens the office's Unix socket, readable only by the current user.
func listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		c.Close()
		return nil, errAlreadyRunning
	}
	_ = os.Remove(path) // stale socket from a crashed run
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// serve reads newline-delimited Events from each connection until l is closed.
func serve(l net.Listener, out func(Event)) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			sc := bufio.NewScanner(c)
			sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
			for sc.Scan() {
				var e Event
				if json.Unmarshal(sc.Bytes(), &e) == nil && e.SessionID != "" {
					out(e)
				}
			}
		}()
	}
}
