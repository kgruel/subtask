package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kgruel/subtask/pkg/subtaskerr"
	"github.com/kgruel/subtask/pkg/task"
)

// knownConfigKeys is the complete set of recognised top-level JSON keys in a
// config file. Legacy keys (harness, options) are still accepted for migration;
// anything else is surfaced as a warning so stale keys don't accumulate silently.
var knownConfigKeys = map[string]struct{}{
	"adapter":       {},
	"provider":      {},
	"model":         {},
	"reasoning":     {},
	"max_workspaces": {},
	"harness":       {}, // legacy
	"options":       {}, // legacy
}

// warnUnknownKeysOnce ensures the project config unknown-key warning fires at
// most once per process, even when LoadConfig is called multiple times.
var warnUnknownKeysOnce sync.Once

// warnUserUnknownKeysOnce is the same guard for the user (~/.subtask) config.
var warnUserUnknownKeysOnce sync.Once

// unknownConfigKeys returns a sorted list of unrecognised top-level keys in
// the JSON blob. Returns nil when data is not a JSON object or all keys are known.
func unknownConfigKeys(data []byte) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var unknown []string
	for k := range raw {
		if _, ok := knownConfigKeys[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

const DefaultMaxWorkspaces = 20

// Config is the project configuration (.subtask/config.json).
type Config struct {
	Adapter       string `json:"adapter"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	Reasoning     string `json:"reasoning,omitempty"`
	MaxWorkspaces int    `json:"max_workspaces"`

	// Legacy fields for migration (read from old configs, never written).
	LegacyHarness string         `json:"harness,omitempty"`
	LegacyOptions map[string]any `json:"options,omitempty"`
}

// AgentSpec is an adapter+model+reasoning bundle. Used as the resolved dispatch
// configuration for an agent or as an ephemeral CLI override.
type AgentSpec struct {
	Adapter   string `json:"adapter,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

// Entry defines a workspace.
type Entry struct {
	Name string // e.g., "workspace-1"
	Path string // e.g., "~/.subtask/workspaces/-Users-foo-code-project--1"
	ID   int    // e.g., 1
}

// LoadConfig loads the effective config (global defaults + optional project overrides).
func LoadConfig() (*Config, error) {
	userPath := task.ConfigPath()
	user, userExists, err := loadConfigFile(userPath)
	if err != nil {
		return nil, fmt.Errorf("subtask: invalid config at %s\n\nFix it with:\n  subtask config --user", userPath)
	}

	// Best-effort project override discovery (requires git; ignored if not in git).
	var project *Config
	var projectPath string
	if root, err := task.GitRootAbs(); err == nil && strings.TrimSpace(root) != "" {
		projectPath = filepath.Join(root, ".subtask", "config.json")
		var projectExists bool
		project, projectExists, err = loadConfigFile(projectPath)
		if err != nil {
			return nil, fmt.Errorf("subtask: invalid project config at %s\n\nFix it with:\n  subtask config --project", projectPath)
		}
		if projectExists {
			if raw, readErr := os.ReadFile(projectPath); readErr == nil {
				if unknown := unknownConfigKeys(raw); len(unknown) > 0 {
					warnUnknownKeysOnce.Do(func() {
						fmt.Fprintf(os.Stderr, "warning: %s contains unknown config keys: %s (ignored)\n",
							projectPath, strings.Join(unknown, ", "))
					})
				}
			}
		}
	}

	if userExists {
		if raw, readErr := os.ReadFile(userPath); readErr == nil {
			if unknown := unknownConfigKeys(raw); len(unknown) > 0 {
				warnUserUnknownKeysOnce.Do(func() {
					hint := ""
					for _, k := range unknown {
						if k == "presets" {
							hint = "\nhint: \"presets\" has been replaced by agents — create .subtask/agents/<name>.yaml for each preset"
							break
						}
					}
					fmt.Fprintf(os.Stderr, "warning: %s contains unknown config keys: %s (ignored)%s\n",
						userPath, strings.Join(unknown, ", "), hint)
				})
			}
		}
	}

	if !userExists || user == nil {
		return nil, subtaskerr.ErrNotConfigured
	}

	effective := mergeConfig(user, project)
	if effective.MaxWorkspaces <= 0 {
		effective.MaxWorkspaces = DefaultMaxWorkspaces
	}
	if err := validateConfig(effective); err != nil {
		return nil, err
	}
	return effective, nil
}

// validateConfig checks intra-config references. Workflow references are
// validated at draft time (they require loading templates from disk).
func validateConfig(_ *Config) error {
	return nil
}

// SaveTo writes the config to a specific path.
// Legacy fields are zeroed before saving so written configs always use the new format.
func (c *Config) SaveTo(path string) error {
	if c.MaxWorkspaces <= 0 {
		c.MaxWorkspaces = DefaultMaxWorkspaces
	}

	// Save a copy without legacy fields.
	save := *c
	save.LegacyHarness = ""
	save.LegacyOptions = nil

	data, err := json.MarshalIndent(&save, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Save writes the config to the global defaults path (~/.subtask/config.json).
func (c *Config) Save() error {
	return c.SaveTo(task.ConfigPath())
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

func mergeConfig(user, project *Config) *Config {
	out := &Config{
		Adapter:       strings.TrimSpace(user.Adapter),
		Provider:      strings.TrimSpace(user.Provider),
		Model:         strings.TrimSpace(user.Model),
		Reasoning:     strings.TrimSpace(user.Reasoning),
		MaxWorkspaces: user.MaxWorkspaces,
	}

	if project == nil {
		return out
	}

	if strings.TrimSpace(project.Adapter) != "" {
		out.Adapter = strings.TrimSpace(project.Adapter)
	}
	if strings.TrimSpace(project.Provider) != "" {
		out.Provider = strings.TrimSpace(project.Provider)
	}
	if strings.TrimSpace(project.Model) != "" {
		out.Model = strings.TrimSpace(project.Model)
	}
	if strings.TrimSpace(project.Reasoning) != "" {
		out.Reasoning = strings.TrimSpace(project.Reasoning)
	}
	if project.MaxWorkspaces > 0 {
		out.MaxWorkspaces = project.MaxWorkspaces
	}
	return out
}

func loadConfigFile(path string) (*Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, true, err
	}
	cfg.migrateLegacy()
	return &cfg, true, nil
}

// MigrateLegacyPublic is the exported entry point for migrateLegacy.
// Use this when manually unmarshaling Config outside of LoadConfig (e.g. readConfigFileOrNil).
func (c *Config) MigrateLegacyPublic() {
	c.migrateLegacy()
}

// migrateLegacy copies values from legacy fields (harness, options) to the new
// top-level fields (adapter, model, reasoning) and clears the legacy fields.
func (c *Config) migrateLegacy() {
	if c.Adapter == "" && strings.TrimSpace(c.LegacyHarness) != "" {
		c.Adapter = strings.TrimSpace(c.LegacyHarness)
	}
	if c.Model == "" && c.LegacyOptions != nil {
		if m, ok := c.LegacyOptions["model"].(string); ok && strings.TrimSpace(m) != "" {
			c.Model = strings.TrimSpace(m)
		}
	}
	if c.Reasoning == "" && c.LegacyOptions != nil {
		if r, ok := c.LegacyOptions["reasoning"].(string); ok && strings.TrimSpace(r) != "" {
			c.Reasoning = strings.TrimSpace(r)
		}
	}
	c.LegacyHarness = ""
	c.LegacyOptions = nil
}
