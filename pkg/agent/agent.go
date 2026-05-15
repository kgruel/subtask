// Package agent loads Agent YAML files from .subtask/agents/<name>.yaml.
//
// An Agent bundles a preset (adapter+model+reasoning) with a prompt that
// defines its role. It is the first half of the Routine + Agent layer
// described in docs/dev/_audit-skill-workflow-primitives.md.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// Agent is a parsed .subtask/agents/<name>.yaml file.
//
// Exactly one of PresetName or PresetInline is non-zero. Exactly one of
// Prompt.Text or Prompt.File is non-empty (enforced at parse time).
type Agent struct {
	// Name is the agent name (file basename without .yaml).
	Name string

	// Description is an optional human-readable summary from the YAML.
	Description string

	// PresetName is the named preset to overlay (resolved against
	// cfg.Presets by the caller). Empty when the agent declares an
	// inline preset block.
	PresetName string

	// PresetInline is the inline preset block. Nil when the agent
	// references a named preset.
	PresetInline *workspace.Preset

	// Prompt holds the agent's role-defining text. Exactly one source.
	Prompt PromptSource
}

// PromptSource declares where the agent's prompt comes from. Exactly one
// of Text or File is non-empty.
type PromptSource struct {
	// Text is the inline prompt body.
	Text string
	// File is a path relative to <repo>/.subtask/ — for example
	// "prompts/planner.md". Validated to exist at load time; its
	// contents are read lazily at BuildPrompt time so prompt-file
	// edits do not require redrafting tasks.
	File string
}

// AgentsDir returns the path to the agents directory.
func AgentsDir() string {
	return filepath.Join(task.ProjectDir(), "agents")
}

// validateAgentName rejects names that would escape .subtask/agents/.
// An agent name must be a simple basename: no path separators (`/`,
// `\`), no traversal segments (`.` / `..`), no absolute paths, and not
// empty. Anything else could let a TASK.md `agent: ../../etc/passwd`
// reach files outside the project — agent names ride in frontmatter
// and may come from less-trusted sources (synced task folders, shared
// routine YAMLs).
func validateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("agent name %q must not be an absolute path", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("agent name %q must not contain path separators — use a simple basename", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("agent name %q is not allowed", name)
	}
	return nil
}

// pathFor returns the YAML path for a given agent name. The caller is
// responsible for having validated `name` via validateAgentName.
func pathFor(name string) string {
	return filepath.Join(AgentsDir(), name+".yaml")
}

// resolvePromptFile validates a prompt.file path against traversal and
// returns its absolute location under .subtask/. The path must:
//   - not be absolute (would bypass the .subtask/ anchor entirely);
//   - resolve, after Clean, to a location contained inside
//     <repo>/.subtask/ (no `..` escape).
//
// Returning an absolute, contained path means callers can read it
// without re-validating. We deliberately do NOT resolve symlinks here —
// if a user drops a symlink inside .subtask/prompts/ pointing outside,
// that's already a trust boundary they crossed by writing into their
// own repo. The check defends against malicious YAML reaching files
// the YAML author shouldn't be able to point at on its own.
func resolvePromptFile(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("prompt.file: empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("prompt.file %q must be relative to .subtask/, not absolute", rel)
	}
	cleaned := filepath.Clean(rel)
	// Anchor at .subtask/ and verify the join stays inside it.
	base := task.ProjectDirAbs()
	abs := filepath.Clean(filepath.Join(base, cleaned))
	relBack, err := filepath.Rel(base, abs)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("prompt.file %q must stay inside .subtask/ (no `..` traversal)", rel)
	}
	return abs, nil
}

// LoadByName reads, parses, and validates an agent.
//
// Returns a clear, actionable error when the file is missing — callers
// re-resolve agents on every send/stage/BuildPrompt, so missing-file
// recovery is in the user's hands.
func LoadByName(name string) (*Agent, error) {
	if err := validateAgentName(name); err != nil {
		return nil, err
	}
	p := pathFor(name)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(".subtask/agents/%s.yaml not found", name)
		}
		return nil, fmt.Errorf("read agent %q: %w", name, err)
	}
	a, err := parseAgent(data)
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", name, err)
	}
	a.Name = name

	// Validate file: path stays under .subtask/ and the target exists.
	if a.Prompt.File != "" {
		fp, err := resolvePromptFile(a.Prompt.File)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", name, err)
		}
		if _, err := os.Stat(fp); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("agent %q: prompt.file %q not found (resolved to %s)", name, a.Prompt.File, fp)
			}
			return nil, fmt.Errorf("agent %q: prompt.file %q: %w", name, a.Prompt.File, err)
		}
	}

	return a, nil
}

// rawAgent is the on-disk YAML shape with the polymorphic preset field
// captured as a raw node so we can decode it as either a string or a
// preset map.
type rawAgent struct {
	Description string    `yaml:"description,omitempty"`
	Preset      yaml.Node `yaml:"preset"`
	Prompt      rawPrompt `yaml:"prompt"`
}

// rawPrompt mirrors the prompt: block. Skill is captured explicitly so
// we can return a clear "deferred" error rather than silently ignoring
// it. (Mutual exclusion with text:/file: is enforced post-parse.)
type rawPrompt struct {
	Text  *string `yaml:"text,omitempty"`
	File  *string `yaml:"file,omitempty"`
	Skill *string `yaml:"skill,omitempty"`
}

// parseAgent decodes and validates YAML bytes into an Agent. Does not
// touch the filesystem — file-existence checks live in LoadByName so
// parse can be tested with pure in-memory fixtures.
func parseAgent(data []byte) (*Agent, error) {
	var raw rawAgent
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	a := &Agent{Description: raw.Description}

	// Preset: required, polymorphic (string ref OR inline block).
	if raw.Preset.IsZero() {
		return nil, fmt.Errorf("missing required field: preset")
	}
	switch raw.Preset.Kind {
	case yaml.ScalarNode:
		var s string
		if err := raw.Preset.Decode(&s); err != nil {
			return nil, fmt.Errorf("preset: %w", err)
		}
		if s == "" {
			return nil, fmt.Errorf("preset: empty string reference")
		}
		a.PresetName = s
	case yaml.MappingNode:
		var p workspace.Preset
		if err := raw.Preset.Decode(&p); err != nil {
			return nil, fmt.Errorf("preset: %w", err)
		}
		if p.Adapter == "" || p.Model == "" {
			return nil, fmt.Errorf("inline preset requires at least adapter and model")
		}
		a.PresetInline = &p
	default:
		return nil, fmt.Errorf("preset: must be a string reference or an inline map")
	}

	// Prompt: required, exactly one of text/file.
	if raw.Prompt.Skill != nil {
		return nil, fmt.Errorf("prompt.skill: source is not yet supported; use text: or file:")
	}
	hasText := raw.Prompt.Text != nil
	hasFile := raw.Prompt.File != nil
	if !hasText && !hasFile {
		return nil, fmt.Errorf("missing required field: prompt (one of text: or file:)")
	}
	if hasText && hasFile {
		return nil, fmt.Errorf("prompt: text: and file: are mutually exclusive — pick one")
	}
	if hasText {
		if strings.TrimSpace(*raw.Prompt.Text) == "" {
			return nil, fmt.Errorf("prompt.text: empty or whitespace-only — provide a non-empty role prompt")
		}
		a.Prompt.Text = *raw.Prompt.Text
	}
	if hasFile {
		if strings.TrimSpace(*raw.Prompt.File) == "" {
			return nil, fmt.Errorf("prompt.file: empty or whitespace-only path")
		}
		a.Prompt.File = *raw.Prompt.File
	}

	return a, nil
}

// ResolvePromptText returns the prompt body for an agent. For text:
// agents this is a no-op; for file: agents it reads the file at call
// time (lazy by design — edits to the prompt file do not require
// redrafting tasks).
//
// The same traversal check that runs at LoadByName is re-applied here,
// not trusted from prior load: ResolvePromptText is the read-the-bytes
// step, and defense in depth means re-validating the path right before
// the read.
func (a *Agent) ResolvePromptText() (string, error) {
	if a.Prompt.Text != "" {
		return a.Prompt.Text, nil
	}
	fp, err := resolvePromptFile(a.Prompt.File)
	if err != nil {
		return "", fmt.Errorf("agent %q: %w", a.Name, err)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("agent %q: prompt.file %q not found (resolved to %s)", a.Name, a.Prompt.File, fp)
		}
		return "", fmt.Errorf("agent %q: read prompt.file %q: %w", a.Name, a.Prompt.File, err)
	}
	return string(data), nil
}
