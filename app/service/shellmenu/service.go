// Package shellmenu registers the Windows Explorer right-click menu entries
// ("Spark 终端打开" / "Spark 编辑器打开") and delivers the resulting launch
// requests (open terminal / open editor at a path) to the frontend.
package shellmenu

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LaunchRequest describes an "open in Spark" request from the shell context menu.
type LaunchRequest struct {
	Kind  string `json:"kind"`  // "terminal" | "editor"
	Path  string `json:"path"`  // absolute path (file or directory)
	IsDir bool   `json:"isDir"` // whether Path is a directory
}

// ShellMenuService holds a pending launch request until the frontend consumes
// it. This covers the first-instance case where the request arrives before the
// Vue frontend has mounted and subscribed to events; for later (second
// instance) launches the request is also emitted live via the "app:open" event.
type ShellMenuService struct {
	mu      sync.Mutex
	pending *LaunchRequest
}

// ServiceName implements application.ServiceName.
func (s *ShellMenuService) ServiceName() string { return "ShellMenuService" }

// Consume returns and clears the pending launch request (nil if none).
func (s *ShellMenuService) Consume() (*LaunchRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.pending
	s.pending = nil
	return r, nil
}

// SetPending stores a request for the frontend to consume on startup.
func (s *ShellMenuService) SetPending(r *LaunchRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = r
}

// ParseArgs extracts a launch request from a process argument list. It accepts
// "--terminal <path>" and "--editor <path>" (the forms written into the
// Windows registry). The path is stat'd so the frontend knows whether it is a
// file or a directory.
func ParseArgs(args []string) *LaunchRequest {
	kind := ""
	path := ""
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--terminal":
			kind, path = "terminal", args[i+1]
		case "--editor":
			kind, path = "editor", args[i+1]
		}
	}
	if kind == "" || strings.TrimSpace(path) == "" {
		return nil
	}
	path = filepath.Clean(path)
	isDir := false
	if st, err := os.Stat(path); err == nil {
		isDir = st.IsDir()
	}
	return &LaunchRequest{Kind: kind, Path: path, IsDir: isDir}
}
