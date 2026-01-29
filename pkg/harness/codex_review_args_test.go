package harness

import "testing"

func TestCodexHarness_buildReviewCommandArgs_DoesNotPassModeFlags(t *testing.T) {
	c := &CodexHarness{Model: "gpt-test", Reasoning: "high"}

	cases := []ReviewTarget{
		{Uncommitted: true},
		{BaseBranch: "dev"},
		{Commit: "abc123"},
		{TaskName: "fix/bug", BaseBranch: "dev"},
	}

	for _, target := range cases {
		flags, positionals := c.buildReviewCommandArgs("/tmp", target, "Focus")

		for _, forbidden := range []string{"--uncommitted", "--base", "--commit"} {
			for _, f := range flags {
				if f == forbidden {
					t.Fatalf("flags unexpectedly contain %q: %v", forbidden, flags)
				}
			}
		}

		if len(positionals) != 1 {
			t.Fatalf("expected exactly 1 positional prompt, got %d: %v", len(positionals), positionals)
		}
		if positionals[0] == "" {
			t.Fatalf("expected non-empty prompt")
		}
	}
}
