package routine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestList_OnlyCanonicalsWhenNoProjectDir(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	summaries, _, err := List()
	require.NoError(t, err)
	require.Len(t, summaries, 3, "expected the 3 built-in canonical routines")

	byName := make(map[string]RoutineSummary, len(summaries))
	for _, s := range summaries {
		byName[s.Name] = s
	}
	for _, name := range []string{"default", "they-plan", "you-plan"} {
		s, ok := byName[name]
		require.True(t, ok, "missing canonical routine %q", name)
		require.Equal(t, SourceCanonical, s.Source)
		require.NotEmpty(t, s.EntryStep, "canonical %q must have an entry step", name)
		require.NotEmpty(t, s.Description, "canonical %q must have a description", name)
	}
}

func TestList_ProjectShadowOverridesCanonical(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))

	// Shadow the built-in "default" with a project-local version.
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "default.yaml"), []byte(`
name: default
description: Project shadow of default
steps:
  - id: doing
  - id: done
    kind: terminal
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)

	byName := make(map[string]RoutineSummary, len(summaries))
	for _, s := range summaries {
		byName[s.Name] = s
	}

	s, ok := byName["default"]
	require.True(t, ok)
	require.Equal(t, SourceShadow, s.Source, "shadowed canonical must have source=shadow")
	require.Equal(t, "Project shadow of default", s.Description)
	require.Equal(t, "doing", s.EntryStep)

	// Other canonicals stay as canonical.
	require.Equal(t, SourceCanonical, byName["they-plan"].Source)
	require.Equal(t, SourceCanonical, byName["you-plan"].Source)
}

func TestList_ProjectOnlyRoutine(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "smoke-custom.yaml"), []byte(`
name: smoke-custom
description: Custom project routine
steps:
  - id: a
  - id: done
    kind: terminal
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)

	byName := make(map[string]RoutineSummary, len(summaries))
	for _, s := range summaries {
		byName[s.Name] = s
	}

	s, ok := byName["smoke-custom"]
	require.True(t, ok)
	require.Equal(t, SourceProject, s.Source)
	require.Equal(t, "a", s.EntryStep)

	// Canonicals still present.
	require.Equal(t, SourceCanonical, byName["default"].Source)
}

func TestList_BadProjectRoutineIsWarnedNotFatal(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))

	// Valid project routine.
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "good-routine.yaml"), []byte(`
name: good-routine
description: A valid routine
steps:
  - id: start
  - id: done
    kind: terminal
`), 0o644))

	// Bad routine: uses legacy 'to:' in a gate option (parse error).
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "bad-gate.yaml"), []byte(`
name: bad-gate
description: Legacy gate
steps:
  - id: review
    kind: gate
    options:
      - { name: approve, to: done }
  - id: done
    kind: terminal
`), 0o644))

	summaries, warnings, err := List()
	require.NoError(t, err, "directory-level errors should not occur")

	// Bad routine must not appear in summaries.
	byName := make(map[string]RoutineSummary, len(summaries))
	for _, s := range summaries {
		byName[s.Name] = s
	}
	require.NotContains(t, byName, "bad-gate", "failed routine must be omitted")

	// Canonical routines and good project routine must still be listed.
	for _, name := range []string{"default", "they-plan", "you-plan", "good-routine"} {
		require.Contains(t, byName, name, "routine %q must still be listed", name)
	}

	// Bad routine's error must appear in warnings.
	require.Len(t, warnings, 1, "expected one warning for the bad routine")
	require.Contains(t, warnings[0], "bad-gate")
}

func TestList_ResultsSortedAlphabetically(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "aaa-first.yaml"), []byte(`
name: aaa-first
description: Comes first alphabetically
steps:
  - id: start
  - id: done
    kind: terminal
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)
	require.Greater(t, len(summaries), 1)
	require.Equal(t, "aaa-first", summaries[0].Name)
}
