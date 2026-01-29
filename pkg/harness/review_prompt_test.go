package harness

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupRepoWithDevBase(t *testing.T) (dir string, baseSHA string) {
	t.Helper()

	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")

	dir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup git %v failed: %v\n%s", args, err, out)
		}
	}

	// Some environments configure init.defaultBranch=main; force master to keep this stable.
	{
		cmd := exec.Command("git", "init", "-b", "master")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = out
			run("init")
		}
	}
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "Initial commit")

	baseSHA = gitOut(t, dir, "rev-parse", "HEAD")
	run("branch", "dev", baseSHA)

	// Add a commit on master so merge-base(master, dev) is deterministic (= baseSHA).
	run("commit", "--allow-empty", "-m", "Work on master")

	return dir, baseSHA
}

func TestBuildReviewPrompt_Uncommitted(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Uncommitted: true}, "")
	assert.Equal(t, "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.", prompt)
}

func TestBuildReviewPrompt_UncommittedWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Uncommitted: true}, "Focus on security vulnerabilities")
	assert.Equal(t, "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.\n\nFocus on security vulnerabilities", prompt)
}

func TestBuildReviewPrompt_BaseBranch_NoGitRepo(t *testing.T) {
	// When git.MergeBase fails (e.g., no git repo), we get the fallback prompt
	prompt := buildReviewPrompt("/nonexistent/path", ReviewTarget{BaseBranch: "main"}, "")
	assert.Equal(t, "Review the code changes against the base branch 'main'. Start by finding the merge diff between the current branch and main's upstream e.g. (`git merge-base HEAD \"$(git rev-parse --abbrev-ref \"main@{upstream}\")\"`), then run `git diff` against that SHA to see what changes we would merge into the main branch. Provide prioritized, actionable findings.", prompt)
}

func TestBuildReviewPrompt_BaseBranchWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("/nonexistent/path", ReviewTarget{BaseBranch: "develop"}, "Check for race conditions")

	// Should have both base branch info and custom instructions
	assert.Contains(t, prompt, "develop")
	assert.Contains(t, prompt, "Check for race conditions")
	assert.Contains(t, prompt, "git rev-parse --abbrev-ref")

	// Instructions should be separated from base prompt
	parts := strings.Split(prompt, "\n\n")
	assert.Len(t, parts, 2)
}

func TestBuildReviewPrompt_BaseBranch_WithMergeBase(t *testing.T) {
	dir, baseSHA := setupRepoWithDevBase(t)

	prompt := buildReviewPrompt(dir, ReviewTarget{BaseBranch: "dev"}, "")
	expected := "Review the code changes against the base branch 'dev'. The merge base commit for this comparison is " + baseSHA + ". Run `git diff " + baseSHA + "` to inspect the changes relative to dev. Provide prioritized, actionable findings."
	assert.Equal(t, expected, prompt)
}

func TestBuildReviewPrompt_Commit(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Commit: "abc1234"}, "")
	assert.Equal(t, "Review the code changes introduced by commit abc1234. Provide prioritized, actionable findings.", prompt)
}

func TestBuildReviewPrompt_CommitWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Commit: "def5678"}, "Check security")
	assert.Equal(t, "Review the code changes introduced by commit def5678. Provide prioritized, actionable findings.\n\nCheck security", prompt)
}

func TestBuildReviewPrompt_Task_WithMergeBase(t *testing.T) {
	dir, baseSHA := setupRepoWithDevBase(t)

	prompt := buildReviewPrompt(dir, ReviewTarget{TaskName: "fix/bug", BaseBranch: "dev"}, "")
	expected := "Review the code changes for subtask task 'fix/bug' against the base branch 'dev'. The merge base commit for this comparison is " + baseSHA + ". Run `git diff " + baseSHA + "` to inspect the changes relative to dev. Provide prioritized, actionable findings."
	assert.Equal(t, expected, prompt)
}

func TestBuildReviewPrompt_EmptyTarget_DefaultsToUncommitted(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{}, "")
	assert.Equal(t, "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.", prompt)
}
