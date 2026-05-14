// Package routine loads Routine YAML files from .subtask/routines/<name>.yaml.
//
// A Routine composes a sequence of Steps with conditional loopback edges,
// optional gate decisions, and explicit terminal markers. It is the second
// half of the Routine + Agent layer described in
// docs/dev/_audit-skill-workflow-primitives.md.
//
// Routines and Workflows coexist during the transition (step 4 of the
// refactor); pkg/workflow.Stage stays untouched here. Step 5 deletes Stage.
package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// Step kinds. Regular (default) steps may auto-dispatch and advance.
// Gate and terminal steps never auto-dispatch.
const (
	KindGate     = "gate"
	KindTerminal = "terminal"
)

// Routine is a parsed .subtask/routines/<name>.yaml file.
type Routine struct {
	// Name is the routine name (file basename without .yaml).
	Name string

	// DefaultPrompt is the routine's project-wide brief, used as the
	// `## Project` block in BuildPrompt for routine-driven tasks
	// (replaces .subtask/WORKER.md for those tasks). Nil when omitted.
	DefaultPrompt *PromptSource

	// Steps is the routine's declaration order. Always ≥1.
	Steps []Step
}

// Step is a single node in a routine. Distinct from pkg/workflow.Stage:
// Stage is part of the legacy substrate and stays untouched until step 5.
type Step struct {
	// ID is unique within the routine.
	ID string `yaml:"id"`

	// Kind controls dispatch behavior: "" (regular), "gate", "terminal".
	Kind string `yaml:"kind,omitempty"`

	// Agent references .subtask/agents/<name>.yaml (resolved by
	// pkg/agent.LoadByName at send/BuildPrompt time). Mutually exclusive
	// with Preset.
	Agent string `yaml:"agent,omitempty"`

	// Preset names a cfg.Presets entry. Mutually exclusive with Agent.
	Preset string `yaml:"preset,omitempty"`

	// Consumes / Produces are inert metadata for downstream tooling
	// (mirrors pkg/workflow.Stage). Produces is a single filename; a step
	// can produce at most one artifact for v1.
	Consumes []string `yaml:"consumes,omitempty"`
	Produces string   `yaml:"produces,omitempty"`

	// Advance controls automatic step progression. "auto" is the only
	// recognized value; anything else is a no-op.
	Advance string `yaml:"advance,omitempty"`

	// Notify mirrors workflow.Stage.Notify — when explicitly false,
	// worker replies during this step do not surface via subtask unread.
	Notify *bool `yaml:"notify,omitempty"`

	// WorkerInstructions / WorkerContext mirror workflow.Stage semantics:
	// instructions trigger auto-dispatch on entry; context rides along
	// without triggering dispatch.
	WorkerInstructions string `yaml:"worker_instructions,omitempty"`
	WorkerContext      string `yaml:"worker_context,omitempty"`

	// Branches are loopback / forward edges evaluated in declaration
	// order. Allowed only on regular steps that also set Produces.
	Branches []Branch `yaml:"branches,omitempty"`

	// Options is required on gate steps and forbidden elsewhere.
	Options []Option `yaml:"options,omitempty"`

	// Surface defaults to true for terminal/gate steps. An explicit
	// false marks the step as silent (no unread surface).
	Surface *bool `yaml:"surface,omitempty"`
}

// Branch is a conditional edge on a regular step. Only "artifact.field"
// predicates are supported in v1: read the produced artifact's YAML
// frontmatter and treat Field as a bool key.
type Branch struct {
	To    string `yaml:"to"`
	When  string `yaml:"when"`
	Field string `yaml:"field"`
}

// Option is a single choice on a gate step.
type Option struct {
	Name string `yaml:"name"`
	To   string `yaml:"to"`
}

// PromptSource declares where a routine's default_prompt comes from.
// Exactly one of Text or File is non-empty after parsing.
//
// Mirrors pkg/agent.PromptSource by shape; kept local so the routine
// package doesn't drag agent's loader into its dependency surface.
type PromptSource struct {
	Text string
	File string
}

// RoutinesDir returns the path to the routines directory.
func RoutinesDir() string {
	return filepath.Join(task.ProjectDir(), "routines")
}

// validateRoutineName rejects names that would escape .subtask/routines/.
// Mirrors pkg/agent.validateAgentName.
func validateRoutineName(name string) error {
	if name == "" {
		return fmt.Errorf("routine name is empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("routine name %q must not be an absolute path", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("routine name %q must not contain path separators — use a simple basename", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("routine name %q is not allowed", name)
	}
	return nil
}

// pathFor returns the YAML path for a given routine name. The caller is
// responsible for having validated `name`.
func pathFor(name string) string {
	return filepath.Join(RoutinesDir(), name+".yaml")
}

// resolvePromptFile validates a default_prompt.file path against
// traversal and returns its absolute location under .subtask/. Mirrors
// pkg/agent.resolvePromptFile so the trust boundary stays consistent
// across both primitives.
func resolvePromptFile(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("default_prompt.file: empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("default_prompt.file %q must be relative to .subtask/, not absolute", rel)
	}
	cleaned := filepath.Clean(rel)
	base := task.ProjectDirAbs()
	abs := filepath.Clean(filepath.Join(base, cleaned))
	relBack, err := filepath.Rel(base, abs)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("default_prompt.file %q must stay inside .subtask/ (no `..` traversal)", rel)
	}
	return abs, nil
}

// validateArtifactPath checks that a `produces:` / `consumes:` path is
// safe to join against the task folder at runtime. A routine can be
// loaded from a less-trusted source (synced .subtask/ folder, shared
// routines repo), so the same hardening that gates agent prompt.file
// applies here: reject absolute paths, traversal, and empty/whitespace.
//
// We do not anchor against an actual task folder (it doesn't exist at
// load time) — pure path-shape validation suffices because the runner
// always Joins against an absolute task folder under .subtask/tasks/,
// and Clean has already collapsed any non-escaping `..` segments.
func validateArtifactPath(p, field string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("%s: empty or whitespace-only path", field)
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("%s %q must be relative to the task folder, not absolute", field, p)
	}
	cleaned := filepath.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || strings.Contains(cleaned, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return fmt.Errorf("%s %q must stay inside the task folder (no `..` traversal)", field, p)
	}
	// Defensive: on Unix backslash isn't a separator, but a YAML author
	// may try `..\foo`. Reject anything containing the alternate separator
	// to avoid platform-dependent acceptance.
	if strings.ContainsAny(cleaned, `\`) {
		return fmt.Errorf("%s %q must use forward slashes only", field, p)
	}
	return nil
}

// LoadByName reads, parses, and validates a routine.
func LoadByName(name string) (*Routine, error) {
	if err := validateRoutineName(name); err != nil {
		return nil, err
	}
	p := pathFor(name)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(".subtask/routines/%s.yaml not found", name)
		}
		return nil, fmt.Errorf("read routine %q: %w", name, err)
	}
	r, err := parseRoutine(data)
	if err != nil {
		return nil, fmt.Errorf("routine %q: %w", name, err)
	}
	r.Name = name

	// If default_prompt.file is set, validate the path stays under .subtask/
	// AND the file exists at load time. (Lazy content read happens at
	// BuildPrompt time — same pattern as agent.Prompt.File.)
	if r.DefaultPrompt != nil && r.DefaultPrompt.File != "" {
		fp, err := resolvePromptFile(r.DefaultPrompt.File)
		if err != nil {
			return nil, fmt.Errorf("routine %q: %w", name, err)
		}
		if _, err := os.Stat(fp); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("routine %q: default_prompt.file %q not found (resolved to %s)", name, r.DefaultPrompt.File, fp)
			}
			return nil, fmt.Errorf("routine %q: default_prompt.file %q: %w", name, r.DefaultPrompt.File, err)
		}
	}

	return r, nil
}

// rawRoutine is the on-disk YAML shape with default_prompt captured as
// a yaml.Node so we can decode it polymorphically (string OR map).
type rawRoutine struct {
	Name          string    `yaml:"name"`
	DefaultPrompt yaml.Node `yaml:"default_prompt"`
	Steps         []Step    `yaml:"steps"`
}

// rawPromptSource mirrors the default_prompt: map shape.
type rawPromptSource struct {
	Text  *string `yaml:"text,omitempty"`
	File  *string `yaml:"file,omitempty"`
	Skill *string `yaml:"skill,omitempty"`
}

// parseRoutine decodes and validates YAML bytes into a Routine. Does not
// touch the filesystem; file-existence checks live in LoadByName.
func parseRoutine(data []byte) (*Routine, error) {
	var raw rawRoutine
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	r := &Routine{
		Steps: raw.Steps,
	}

	// default_prompt: optional, polymorphic (scalar string OR map).
	if !raw.DefaultPrompt.IsZero() {
		ps, err := decodeDefaultPrompt(&raw.DefaultPrompt)
		if err != nil {
			return nil, err
		}
		r.DefaultPrompt = ps
	}

	if len(r.Steps) == 0 {
		return nil, fmt.Errorf("routine has no steps")
	}

	if err := validateSteps(r.Steps); err != nil {
		return nil, err
	}

	return r, nil
}

// decodeDefaultPrompt accepts either `default_prompt: "inline text"` or
// `default_prompt: { text: "...", file: "..." }`. Validation matches the
// agent prompt-source rules: exactly one source, no empty/whitespace,
// skill: deferred.
func decodeDefaultPrompt(node *yaml.Node) (*PromptSource, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return nil, fmt.Errorf("default_prompt: %w", err)
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("default_prompt: empty or whitespace-only — provide a non-empty prompt")
		}
		return &PromptSource{Text: s}, nil
	case yaml.MappingNode:
		var raw rawPromptSource
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("default_prompt: %w", err)
		}
		if raw.Skill != nil {
			return nil, fmt.Errorf("default_prompt.skill: source is not yet supported; use text: or file:")
		}
		hasText := raw.Text != nil
		hasFile := raw.File != nil
		if !hasText && !hasFile {
			return nil, fmt.Errorf("default_prompt: missing source (one of text: or file:)")
		}
		if hasText && hasFile {
			return nil, fmt.Errorf("default_prompt: text: and file: are mutually exclusive — pick one")
		}
		ps := &PromptSource{}
		if hasText {
			if strings.TrimSpace(*raw.Text) == "" {
				return nil, fmt.Errorf("default_prompt.text: empty or whitespace-only — provide a non-empty prompt")
			}
			ps.Text = *raw.Text
		}
		if hasFile {
			if strings.TrimSpace(*raw.File) == "" {
				return nil, fmt.Errorf("default_prompt.file: empty or whitespace-only path")
			}
			ps.File = *raw.File
		}
		return ps, nil
	default:
		return nil, fmt.Errorf("default_prompt: must be a string or a map with text:/file:")
	}
}

// validateSteps enforces all schema rules across steps.
func validateSteps(steps []Step) error {
	ids := make(map[string]struct{}, len(steps))
	for i, s := range steps {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("steps[%d]: missing id", i)
		}
		if _, dup := ids[s.ID]; dup {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		ids[s.ID] = struct{}{}

		switch s.Kind {
		case "", KindGate, KindTerminal:
		default:
			return fmt.Errorf("step %q: unknown kind %q (allowed: \"\", %q, %q)", s.ID, s.Kind, KindGate, KindTerminal)
		}

		if s.Kind == KindGate {
			if s.Agent != "" || s.Preset != "" {
				return fmt.Errorf("step %q: gate steps cannot reference agent: or preset:", s.ID)
			}
			if len(s.Options) == 0 {
				return fmt.Errorf("step %q: gate steps require at least one option", s.ID)
			}
			if s.Produces != "" || len(s.Branches) > 0 {
				return fmt.Errorf("step %q: gate steps cannot declare produces: or branches:", s.ID)
			}
			if s.Advance == "auto" {
				return fmt.Errorf("step %q: gate steps cannot use advance: auto", s.ID)
			}
			// Gates don't auto-dispatch, so worker_instructions and
			// worker_context have no effect on entry. The schema docs
			// describe these as dispatch-bound; reject on gates so a
			// routine author doesn't get a silent no-op.
			if strings.TrimSpace(s.WorkerInstructions) != "" || strings.TrimSpace(s.WorkerContext) != "" {
				return fmt.Errorf("step %q: gate steps cannot declare worker_instructions: or worker_context: (gates don't dispatch)", s.ID)
			}
		}

		if s.Kind == KindTerminal {
			if s.Agent != "" || s.Preset != "" {
				return fmt.Errorf("step %q: terminal steps cannot reference agent: or preset:", s.ID)
			}
			if len(s.Branches) > 0 || len(s.Options) > 0 {
				return fmt.Errorf("step %q: terminal steps cannot declare branches: or options:", s.ID)
			}
			if s.Produces != "" || len(s.Consumes) > 0 {
				return fmt.Errorf("step %q: terminal steps cannot declare produces: or consumes:", s.ID)
			}
			// Terminals end auto-advance and don't dispatch. advance: auto
			// is meaningless; worker_instructions / worker_context are
			// dispatch-bound and never fire. Reject explicitly so a
			// misconfigured routine fails at load, not at "wait, why
			// didn't the routine continue past the terminal".
			if s.Advance == "auto" {
				return fmt.Errorf("step %q: terminal steps cannot use advance: auto (terminals end the routine)", s.ID)
			}
			if strings.TrimSpace(s.WorkerInstructions) != "" || strings.TrimSpace(s.WorkerContext) != "" {
				return fmt.Errorf("step %q: terminal steps cannot declare worker_instructions: or worker_context: (terminals don't dispatch)", s.ID)
			}
		}

		if s.Kind == "" {
			if len(s.Options) > 0 {
				return fmt.Errorf("step %q: options: is only valid on gate steps", s.ID)
			}
			// Surface is documented as a terminal/gate field — author
			// intent on a regular step is unclear (the unread check
			// skips regular steps for surface), so reject and direct
			// the author to notify: false for the dispatch-step
			// equivalent.
			if s.Surface != nil {
				return fmt.Errorf("step %q: surface: is only valid on terminal or gate steps; use notify: false to silence a regular step", s.ID)
			}
			if s.Agent != "" && s.Preset != "" {
				return fmt.Errorf("step %q: agent: and preset: are mutually exclusive — pick one", s.ID)
			}
			if len(s.Branches) > 0 && strings.TrimSpace(s.Produces) == "" {
				return fmt.Errorf("step %q: branches: requires produces: (the artifact whose frontmatter is read)", s.ID)
			}
			// Path-shape hardening for artifact references. Same trust
			// boundary as agent prompt.file: routine YAMLs ride in
			// shareable .subtask/ trees and may come from less-trusted
			// sources.
			if s.Produces != "" {
				if err := validateArtifactPath(s.Produces, fmt.Sprintf("step %q: produces", s.ID)); err != nil {
					return err
				}
			}
			for j, c := range s.Consumes {
				if err := validateArtifactPath(c, fmt.Sprintf("step %q: consumes[%d]", s.ID, j)); err != nil {
					return err
				}
			}
		}

		for j, b := range s.Branches {
			if strings.TrimSpace(b.To) == "" {
				return fmt.Errorf("step %q: branches[%d]: missing to:", s.ID, j)
			}
			if b.When != "artifact.field" {
				return fmt.Errorf("step %q: branches[%d]: only when: artifact.field is supported in v1 (got %q)", s.ID, j, b.When)
			}
			if strings.TrimSpace(b.Field) == "" {
				return fmt.Errorf("step %q: branches[%d]: missing field:", s.ID, j)
			}
		}

		for j, o := range s.Options {
			if strings.TrimSpace(o.Name) == "" {
				return fmt.Errorf("step %q: options[%d]: missing name:", s.ID, j)
			}
			if strings.TrimSpace(o.To) == "" {
				return fmt.Errorf("step %q: options[%d]: missing to:", s.ID, j)
			}
		}
	}

	// Resolve branch/option `to:` targets against known step ids. Done in
	// a second pass so forward references work.
	for _, s := range steps {
		for j, b := range s.Branches {
			if _, ok := ids[b.To]; !ok {
				return fmt.Errorf("step %q: branches[%d].to %q does not match any step id", s.ID, j, b.To)
			}
		}
		for j, o := range s.Options {
			if _, ok := ids[o.To]; !ok {
				return fmt.Errorf("step %q: options[%d].to %q does not match any step id", s.ID, j, o.To)
			}
		}
	}
	return nil
}

// ValidateReferences resolves every step's agent: and preset: against
// the actual environment (filesystem agent YAMLs, cfg.Presets). Called
// at draft time so a typo in a later step's reference fails fast at
// the boundary instead of mid-routine after worker rounds.
//
// Matches the workflow draft behavior: workflow drafting validates all
// stage presets up front. parseRoutine handles schema shape (no cfg
// dependency); ValidateReferences handles environmental lookups.
func (r *Routine) ValidateReferences(cfg *workspace.Config) error {
	for _, s := range r.Steps {
		if s.Agent != "" {
			ag, err := agent.LoadByName(s.Agent)
			if err != nil {
				return fmt.Errorf("routine %q step %q: %w", r.Name, s.ID, err)
			}
			// If the agent's preset is a string ref, check it resolves
			// here too — surfaces the same broken reference at draft
			// rather than at the first cross-stage swap.
			if ag.PresetName != "" {
				if _, ok := cfg.Presets[ag.PresetName]; !ok {
					return fmt.Errorf("routine %q step %q: agent %q references unknown preset %q\n\nAvailable: %s",
						r.Name, s.ID, s.Agent, ag.PresetName, workspace.PresetNames(cfg))
				}
			}
		}
		if s.Preset != "" {
			if _, ok := cfg.Presets[s.Preset]; !ok {
				return fmt.Errorf("routine %q step %q: unknown preset %q\n\nAvailable: %s",
					r.Name, s.ID, s.Preset, workspace.PresetNames(cfg))
			}
		}
	}
	return nil
}

// GetStep returns the step with the given id, or nil if not found.
func (r *Routine) GetStep(id string) *Step {
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			return &r.Steps[i]
		}
	}
	return nil
}

// stepIndex returns the index of the step with the given id, or -1.
func (r *Routine) stepIndex(id string) int {
	for i := range r.Steps {
		if r.Steps[i].ID == id {
			return i
		}
	}
	return -1
}

// EntryStep returns the first step id (the routine's entry point).
func (r *Routine) EntryStep() string {
	if len(r.Steps) == 0 {
		return ""
	}
	return r.Steps[0].ID
}

// nextInOrder returns the step id immediately following currentID in
// declaration order, or "" if currentID is the last (or unknown).
func (r *Routine) nextInOrder(currentID string) string {
	idx := r.stepIndex(currentID)
	if idx < 0 || idx >= len(r.Steps)-1 {
		return ""
	}
	return r.Steps[idx+1].ID
}

// StepIDs returns all step ids in declaration order.
func (r *Routine) StepIDs() []string {
	out := make([]string, len(r.Steps))
	for i := range r.Steps {
		out[i] = r.Steps[i].ID
	}
	return out
}

// IsSilent returns true when the step explicitly opted out of
// notification. Default (nil pointer) is "notify on".
func (s *Step) IsSilent() bool {
	return s != nil && s.Notify != nil && !*s.Notify
}

// IsSurfaced returns true when this step's `surface:` field permits
// the step to register a worker reply as unread. Applies to both
// terminal and gate steps per the schema (`surface: *bool` on Step).
// Defaults to true (a nil pointer means "surface this step"); explicit
// `surface: false` disables.
//
// Earlier name was IsTerminalSurfaced, but the field is generic — both
// terminals and gates can opt out, and unread.go was missing the gate
// half of the check.
func (s *Step) IsSurfaced() bool {
	if s == nil {
		return false
	}
	if s.Surface == nil {
		return true
	}
	return *s.Surface
}

// ResolveDefaultPromptText returns the routine's default prompt body
// for use as the `## Project` block. Text-source routines return
// immediately; file-source routines re-validate traversal and read
// lazily so prompt edits don't require redrafting tasks (same pattern
// as agent.ResolvePromptText).
func (r *Routine) ResolveDefaultPromptText() (string, error) {
	if r.DefaultPrompt == nil {
		return "", nil
	}
	if r.DefaultPrompt.Text != "" {
		return r.DefaultPrompt.Text, nil
	}
	fp, err := resolvePromptFile(r.DefaultPrompt.File)
	if err != nil {
		return "", fmt.Errorf("routine %q: %w", r.Name, err)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("routine %q: default_prompt.file %q not found (resolved to %s)", r.Name, r.DefaultPrompt.File, fp)
		}
		return "", fmt.Errorf("routine %q: read default_prompt.file %q: %w", r.Name, r.DefaultPrompt.File, err)
	}
	return string(data), nil
}
