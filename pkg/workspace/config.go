package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zippoxer/subtask/pkg/task"
)

const DefaultMaxWorkspaces = 20

// Config is the project configuration (.subtask/config.json).
type Config struct {
	Harness       string         `json:"harness"`
	MaxWorkspaces int            `json:"max_workspaces"`
	Options       map[string]any `json:"options,omitempty"`
}

// Entry defines a workspace.
type Entry struct {
	Name string // e.g., "workspace-1"
	Path string // e.g., "~/.subtask/workspaces/-Users-foo-code-project--1"
	ID   int    // e.g., 1
}

// LoadConfig loads the project config from .subtask/config.json.
func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(task.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not initialized\n\nRun 'subtask init' first")
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.MaxWorkspaces <= 0 {
		cfg.MaxWorkspaces = DefaultMaxWorkspaces
	}
	return &cfg, nil
}

// Save writes the config to .subtask/config.json.
func (c *Config) Save() error {
	if err := os.MkdirAll(task.ProjectDir(), 0755); err != nil {
		return err
	}

	if c.MaxWorkspaces <= 0 {
		c.MaxWorkspaces = DefaultMaxWorkspaces
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(task.ConfigPath(), data, 0644)
}

// ListWorkspaces discovers workspaces for the current project by globbing.
func ListWorkspaces() ([]Entry, error) {
	repoRoot := task.ProjectRoot()
	escapedPath := task.EscapePath(repoRoot)
	pattern := filepath.Join(task.WorkspacesDir(), escapedPath+"--*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, path := range matches {
		base := filepath.Base(path)
		// Extract ID from "...-escaped-path--N"
		if idx := strings.LastIndex(base, "--"); idx != -1 {
			idStr := base[idx+2:]
			if id, err := strconv.Atoi(idStr); err == nil {
				entries = append(entries, Entry{
					Name: fmt.Sprintf("workspace-%d", id),
					Path: path,
					ID:   id,
				})
			}
		}
	}

	// Sort by ID
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	return entries, nil
}
