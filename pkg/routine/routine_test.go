package routine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestParseRoutine_LinearValid(t *testing.T) {
	data := []byte(`name: linear
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
  - id: review
    agent: reviewer
    advance: auto
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Len(t, r.Steps, 3)
	require.Equal(t, "plan", r.EntryStep())
	require.Equal(t, "PLAN.md", r.GetStep("plan").Produces)
	require.Equal(t, "auto", r.GetStep("plan").Advance)
	require.Equal(t, KindTerminal, r.GetStep("done").Kind)
}

func TestParseRoutine_LoopbackBranch(t *testing.T) {
	data := []byte(`name: loop
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: plan
        when: artifact.field
        field: needs_more_data
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Len(t, r.GetStep("plan").Branches, 1)
	require.Equal(t, "plan", r.GetStep("plan").Branches[0].To)
	require.Equal(t, "needs_more_data", r.GetStep("plan").Branches[0].Field)
}

func TestParseRoutine_GateWithOptions(t *testing.T) {
	data := []byte(`name: gated
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, next: done }
      - { name: revise,  next: plan }
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	rev := r.GetStep("review")
	require.NotNil(t, rev)
	require.Equal(t, KindGate, rev.Kind)
	require.Len(t, rev.Options, 2)
	require.Equal(t, "approve", rev.Options[0].Name)
	require.Equal(t, "done", rev.Options[0].Next)
}

func TestParseRoutine_RejectsLegacyOptionTo(t *testing.T) {
	data := []byte(`name: legacy
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, to: done }
      - { name: revise,  to: plan }
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "next:")
	require.Contains(t, err.Error(), "to:")
}

func TestParseRoutine_RejectsNoTerminalStep(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "no-terminal.yaml"), []byte(`name: no-terminal
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    agent: reviewer
`), 0o644))

	_, err := LoadByName("no-terminal")
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal")
	require.Contains(t, err.Error(), "no-terminal")
}

func TestParseRoutine_TerminalSurfaceDefaultsTrue(t *testing.T) {
	data := []byte(`name: term
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Nil(t, r.GetStep("done").Surface)
	require.True(t, r.GetStep("done").IsSurfaced(), "terminal default surface must be true")
}

func TestParseRoutine_TerminalSurfaceExplicitFalse(t *testing.T) {
	data := []byte(`name: term
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
    surface: false
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	done := r.GetStep("done")
	require.NotNil(t, done.Surface)
	require.False(t, *done.Surface)
	require.False(t, done.IsSurfaced())
}

func TestParseRoutine_BranchWithoutProduces(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
    branches:
      - to: plan
        when: artifact.field
        field: x
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "branches:")
	require.Contains(t, err.Error(), "produces:")
}

func TestParseRoutine_GateWithoutOptions(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: review
    kind: gate
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "option")
}

func TestParseRoutine_DuplicateStepIDs(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: plan
    agent: planner2
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate step id")
}

func TestParseRoutine_StepWithPresetIsUnknownKey(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    preset: opus-high
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "preset")
}

func TestParseRoutine_BranchToUnknownStep(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: ghost
        when: artifact.field
        field: x
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), "does not match")
}

func TestParseRoutine_OptionToUnknownStep(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, next: ghost }
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), "does not match")
}

func TestParseRoutine_UnknownStepKey(t *testing.T) {
	// "next:" is not a recognised step key — authors sometimes write it
	// believing it controls sequencing, but ordering is implicit.
	data := []byte(`name: bad
steps:
  - id: work
    next: done
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"next"`, "error must name the offending key")
	require.Contains(t, err.Error(), "line", "error must include a line reference")
}

func TestLoadByName_UnknownStepKeyIncludesFilePath(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "bad-step.yaml"), []byte(`name: bad-step
steps:
  - id: work
    next: done
  - id: done
    kind: terminal
`), 0o644))

	_, err := LoadByName("bad-step")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad-step", "error must reference the routine name or file")
	require.Contains(t, err.Error(), `"next"`, "error must name the offending key")
	require.Contains(t, err.Error(), "line", "error must include a line reference")
}

func TestParseRoutine_UnknownRoutineKey(t *testing.T) {
	// "flow:" is not a recognised top-level routine key.
	data := []byte(`name: bad
flow: sequential
steps:
  - id: work
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), `"flow"`, "error must name the offending key")
	require.Contains(t, err.Error(), "line", "error must include a line reference")
}

func TestParseRoutine_BranchUnsupportedWhen(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: plan
        when: artifact.exists
        field: x
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "artifact.field")
}

func TestParseRoutine_NoSteps(t *testing.T) {
	data := []byte(`name: empty
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no steps")
}

func TestParseRoutine_UnknownKind(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: x
    kind: hatchery
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown kind")
}

func TestParseRoutine_DefaultPromptStringShorthand(t *testing.T) {
	data := []byte(`name: shorthand
default_prompt: |
  Project conventions: be terse.
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.NotNil(t, r.DefaultPrompt)
	require.Contains(t, r.DefaultPrompt.Text, "be terse")
	require.Empty(t, r.DefaultPrompt.File)
}

func TestParseRoutine_DefaultPromptTextMap(t *testing.T) {
	data := []byte(`name: m
default_prompt:
  text: Inline body.
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Equal(t, "Inline body.", r.DefaultPrompt.Text)
}

func TestParseRoutine_DefaultPromptFileMap(t *testing.T) {
	data := []byte(`name: m
default_prompt:
  file: prompts/conv.md
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Equal(t, "prompts/conv.md", r.DefaultPrompt.File)
	require.Empty(t, r.DefaultPrompt.Text)
}

func TestParseRoutine_DefaultPromptEmptyText(t *testing.T) {
	data := []byte(`name: m
default_prompt:
  text: ""
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "default_prompt")
}

func TestParseRoutine_DefaultPromptWhitespaceOnly(t *testing.T) {
	data := []byte("name: m\ndefault_prompt: \"   \\n  \"\nsteps:\n  - id: plan\n    agent: planner\n    advance: auto\n")
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "default_prompt")
}

func TestParseRoutine_DefaultPromptBothTextAndFile(t *testing.T) {
	data := []byte(`name: m
default_prompt:
  text: hi
  file: prompts/x.md
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

func TestParseRoutine_DefaultPromptSkillDeferred(t *testing.T) {
	data := []byte(`name: m
default_prompt:
  skill: org-conventions
steps:
  - id: plan
    agent: planner
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not yet supported")
}

// ---- schema-vs-handler coverage --------------------------------------------

func TestParseRoutine_RejectsSurfaceOnRegularStep(t *testing.T) {
	// Surface is documented as a terminal/gate-only field. The unread
	// check skips regular steps for surface, so allowing it on regular
	// steps was silent dead config.
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
    surface: false
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "surface:")
	require.Contains(t, err.Error(), "notify: false")
}

func TestParseRoutine_RejectsWorkerInstructionsOnGate(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    worker_instructions: |
      do nothing
    options:
      - { name: ok, next: done }
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "gate steps cannot declare worker_instructions")
}

func TestParseRoutine_RejectsWorkerContextOnGate(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    worker_context: |
      ride-along
    options:
      - { name: ok, next: done }
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "worker_context")
}

func TestParseRoutine_RejectsWorkerInstructionsOnTerminal(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
    worker_instructions: |
      do nothing
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal steps cannot declare worker_instructions")
}

func TestParseRoutine_RejectsAdvanceAutoOnTerminal(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "terminal steps cannot use advance: auto")
}

// ---- produces / consumes path hardening -------------------------------------

func TestParseRoutine_ProducesTraversal(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    produces: ../escape.md
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "produces")
	require.Contains(t, err.Error(), "traversal")
}

func TestParseRoutine_ProducesAbsolute(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    produces: /etc/passwd
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "produces")
	require.Contains(t, err.Error(), "absolute")
}

func TestParseRoutine_ProducesSubdirAllowed(t *testing.T) {
	// Positive case: a normal nested path under the task folder loads
	// fine. Confirms the validator doesn't over-reject.
	data := []byte(`name: ok
steps:
  - id: plan
    agent: planner
    produces: subdir/foo.md
    advance: auto
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Equal(t, "subdir/foo.md", r.GetStep("plan").Produces)
}

func TestParseRoutine_ProducesWhitespace(t *testing.T) {
	data := []byte("name: bad\nsteps:\n  - id: plan\n    agent: planner\n    produces: \"   \"\n    advance: auto\n")
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "produces")
	require.Contains(t, err.Error(), "empty")
}

func TestParseRoutine_ConsumesTraversal(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    consumes: [../escape.md]
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "consumes")
	require.Contains(t, err.Error(), "traversal")
}

func TestParseRoutine_ConsumesAbsolute(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    consumes: [/etc/passwd]
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "consumes")
	require.Contains(t, err.Error(), "absolute")
}

func TestParseRoutine_RejectsConsumesOnGate(t *testing.T) {
	// consumes: renders into the worker prompt now, so a gate (which never
	// dispatches) declaring it is a silent no-op — rejected like
	// worker_instructions. Mirrors TestParseRoutine_RejectsWorkerInstructionsOnGate.
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    consumes: [PLAN.md]
    options:
      - { name: ok, next: done }
  - id: done
    kind: terminal
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "review")
	require.Contains(t, err.Error(), "consumes")
}

func TestValidateArtifactPath(t *testing.T) {
	// Table-driven, OS-independent: path.Clean's output differs from
	// filepath.Clean's on Windows (forward vs. backslash), so assertions
	// below check accept/reject and, where relevant, substring content —
	// never the raw cleaned separator form.
	cases := []struct {
		name    string
		path    string
		wantErr string // substring, or "" for accept
	}{
		{"nested path", "notes/spec.md", ""},
		{"deeper nested path", "a/b/c.md", ""},
		{"single segment", "PLAN.md", ""},
		{"leading traversal", "../x", "traversal"},
		{"nested traversal", "a/../../x", "traversal"},
		{"bare dotdot", "..", "traversal"},
		{"absolute unix path", "/etc/passwd", "absolute"},
		{"windows drive slash-form", "C:/tmp/x.md", "absolute"},
		{"windows drive relative-form", "C:x", "absolute"},
		{"backslash traversal", "..\\x", "forward slashes"},
		{"backslash nested", "a\\b", "forward slashes"},
		{"empty", "", "empty"},
		{"whitespace only", "   ", "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArtifactPath(tc.path, "field")
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseRoutine_ProducesBackslashRejected(t *testing.T) {
	data := []byte(`name: bad
steps:
  - id: plan
    agent: planner
    produces: notes\spec.md
    advance: auto
`)
	_, err := parseRoutine(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "produces")
	require.Contains(t, err.Error(), "forward slashes")
}

func TestParseRoutine_ConsumesNestedAllowed(t *testing.T) {
	data := []byte(`name: ok
steps:
  - id: plan
    agent: planner
    consumes: [notes/spec.md, a/b/c.md]
    advance: auto
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/spec.md", "a/b/c.md"}, r.GetStep("plan").Consumes)
}

func TestParseRoutine_ConsumesOnRegularStepAllowed(t *testing.T) {
	// Guard against over-broad rejection: a regular step declaring consumes
	// must still load clean (the gate/terminal rejection is kind-scoped).
	data := []byte(`name: ok
steps:
  - id: impl
    consumes: [PLAN.md, notes/spec.md]
    worker_instructions: Implement per PLAN.md.
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	require.Equal(t, []string{"PLAN.md", "notes/spec.md"}, r.GetStep("impl").Consumes)
}

// ---- LoadByName: path/name traversal & file existence -----------------------

func TestLoadByName_RejectsTraversalInName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("../../etc/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "path separators")
}

func TestLoadByName_RejectsAbsoluteName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("/etc/passwd")
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "absolute path") || strings.Contains(err.Error(), "path separators"),
		"got: %v", err)
}

func TestLoadByName_RejectsDotDotName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("..")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed")
}

func TestLoadByName_FileNotFound(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), ".subtask/routines/ghost.yaml")
	require.Contains(t, err.Error(), "not found")
}

func TestLoadByName_DefaultPromptFileOutsideSubtask(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "esc.yaml"), []byte(
		`name: esc
default_prompt:
  file: ../../etc/passwd
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`), 0o644))
	_, err := LoadByName("esc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "traversal")
}

func TestLoadByName_DefaultPromptAbsoluteFile(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "abs.yaml"), []byte(
		`name: abs
default_prompt:
  file: /etc/passwd
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`), 0o644))
	_, err := LoadByName("abs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestLoadByName_DefaultPromptFileMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "miss.yaml"), []byte(
		`name: miss
default_prompt:
  file: prompts/does-not-exist.md
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`), 0o644))
	_, err := LoadByName("miss")
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompts/does-not-exist.md")
	require.Contains(t, err.Error(), "not found")
}

// ---- ValidateReferences: agent + preset lookups at draft time --------------

func writeAgent(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(
		`adapter: claude
model: sonnet
prompt:
  text: You are `+name+`.
`), 0o644))
}

func TestValidateReferences_UnknownAgentInLaterStep(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeAgent(t, env.RootDir, "good")

	data := []byte(`name: bad-agent
steps:
  - id: a
    agent: good
    advance: auto
  - id: b
    agent: ghost
    advance: auto
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err, "schema parse should succeed; the agent name is shape-valid")
	r.Name = "bad-agent"

	err = r.ValidateReferences()
	require.Error(t, err)
	require.Contains(t, err.Error(), `step "b"`)
	require.Contains(t, err.Error(), "ghost")
	require.Contains(t, err.Error(), ".subtask/agents/ghost.yaml")
}

func TestValidateReferences_AllResolved(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeAgent(t, env.RootDir, "good")

	data := []byte(`name: ok
steps:
  - id: a
    agent: good
    advance: auto
  - id: done
    kind: terminal
`)
	r, err := parseRoutine(data)
	require.NoError(t, err)
	r.Name = "ok"

	require.NoError(t, r.ValidateReferences())
}

func TestLoadByName_DefaultPromptFileResolves(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "conv.md"), []byte("Be terse."), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "ok.yaml"), []byte(
		`name: ok
default_prompt:
  file: prompts/conv.md
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("ok")
	require.NoError(t, err)

	body, err := r.ResolveDefaultPromptText()
	require.NoError(t, err)
	require.Equal(t, "Be terse.", body)
}

// ---- embedded canonical routines -------------------------------------------

func TestLoadByName_EmbeddedDefaultLoads(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("default")
	require.NoError(t, err)
	require.Equal(t, "default", r.Name)
	require.NotEmpty(t, r.Steps, "default routine must have steps")
	require.Equal(t, "doing", r.EntryStep())
	require.NotNil(t, r.DefaultPrompt, "default routine must have a default_prompt")
}

func TestLoadByName_EmbeddedTheyPlanLoads(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("they-plan")
	require.NoError(t, err)
	require.Equal(t, "they-plan", r.Name)
	require.Equal(t, "plan", r.EntryStep())
	require.NotNil(t, r.DefaultPrompt)
}

func TestLoadByName_EmbeddedYouPlanLoads(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("you-plan")
	require.NoError(t, err)
	require.Equal(t, "you-plan", r.Name)
	require.Equal(t, "plan", r.EntryStep())
	require.NotNil(t, r.DefaultPrompt)
}

func TestLoadByName_EmbeddedDefaultHasReadyTerminal(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("default")
	require.NoError(t, err)
	ready := r.GetStep("ready")
	require.NotNil(t, ready)
	require.Equal(t, KindTerminal, ready.Kind)
	require.NotEmpty(t, ready.Instructions, "ready step must have lead instructions")
}

func TestLoadByName_EmbeddedTheyPlanImplementHasWorkerContext(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("they-plan")
	require.NoError(t, err)
	impl := r.GetStep("implement")
	require.NotNil(t, impl)
	require.NotEmpty(t, impl.WorkerContext, "implement step must have worker_context")
}

func TestLoadByName_ProjectRoutineShadowsEmbedded(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "default.yaml"), []byte(`name: default
steps:
  - id: custom
    instructions: Custom step.
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("default")
	require.NoError(t, err)
	require.Equal(t, "custom", r.EntryStep(), "project routine must shadow embedded")
	custom := r.GetStep("custom")
	require.NotNil(t, custom)
	require.Equal(t, "Custom step.", custom.Instructions)
}

func TestLoadByName_CanonicalSource(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	r, err := LoadByName("default")
	require.NoError(t, err)
	require.Equal(t, SourceCanonical, r.Source, "embedded canonical must report SourceCanonical")
}

func TestLoadByName_ShadowSource(t *testing.T) {
	// A project file named "default" shadows the embedded canonical of the same name.
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "default.yaml"), []byte(`steps:
  - id: custom-doing
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("default")
	require.NoError(t, err)
	require.Equal(t, SourceShadow, r.Source, "project file with same name as embedded canonical must report SourceShadow")
	require.Equal(t, "custom-doing", r.EntryStep(), "project shadow must override the embedded steps")
}

func TestLoadByName_ProjectSource(t *testing.T) {
	// A project file with a name that has no embedded canonical is SourceProject.
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "release.yaml"), []byte(`steps:
  - id: cut
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("release")
	require.NoError(t, err)
	require.Equal(t, SourceProject, r.Source, "project-only routine (no embedded canonical) must report SourceProject")
}

// ---- name: field validation ------------------------------------------------

func TestLoadByName_NameFieldMatchingFilenameOK(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "my-routine.yaml"), []byte(
		`name: my-routine
steps:
  - id: work
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("my-routine")
	require.NoError(t, err)
	require.Equal(t, "my-routine", r.Name)
}

func TestLoadByName_NameFieldAbsentOK(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "no-name.yaml"), []byte(
		`steps:
  - id: work
  - id: done
    kind: terminal
`), 0o644))

	r, err := LoadByName("no-name")
	require.NoError(t, err)
	require.Equal(t, "no-name", r.Name)
}

func TestLoadByName_NameFieldMismatchErrors(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	// File is "bar.yaml" but declares name: foo — mismatch.
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "bar.yaml"), []byte(
		`name: foo
steps:
  - id: work
  - id: done
    kind: terminal
`), 0o644))

	_, err := LoadByName("bar")
	require.Error(t, err)
	require.Contains(t, err.Error(), `name: "foo"`)
	require.Contains(t, err.Error(), `"bar"`)
}
