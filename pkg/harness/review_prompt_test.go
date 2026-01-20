package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildReviewPrompt_Uncommitted(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Uncommitted: true}, "")
	assert.Contains(t, prompt, "Review the current code changes")
	assert.Contains(t, prompt, "staged, unstaged, and untracked")
}

func TestBuildReviewPrompt_UncommittedWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Uncommitted: true}, "Focus on security vulnerabilities")
	assert.Contains(t, prompt, "Review the current code changes")
	assert.Contains(t, prompt, "Focus on security vulnerabilities")
}

func TestBuildReviewPrompt_BaseBranch_NoGitRepo(t *testing.T) {
	// When git.MergeBase fails (e.g., no git repo), we get the fallback prompt
	prompt := buildReviewPrompt("/nonexistent/path", ReviewTarget{BaseBranch: "main"}, "")
	assert.Contains(t, prompt, "Review the code changes against the base branch 'main'")
	assert.Contains(t, prompt, "git merge-base")
}

func TestBuildReviewPrompt_BaseBranchWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("/nonexistent/path", ReviewTarget{BaseBranch: "develop"}, "Check for race conditions")

	// Should have both base branch info and custom instructions
	assert.Contains(t, prompt, "develop")
	assert.Contains(t, prompt, "Check for race conditions")

	// Instructions should be separated from base prompt
	parts := strings.Split(prompt, "\n\n")
	assert.Len(t, parts, 2)
}

func TestBuildReviewPrompt_Commit(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Commit: "abc1234"}, "")
	assert.Contains(t, prompt, "Review the code changes introduced by commit abc1234")
	assert.Contains(t, prompt, "git show abc1234")
}

func TestBuildReviewPrompt_CommitWithInstructions(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{Commit: "def5678"}, "Check security")
	assert.Contains(t, prompt, "def5678")
	assert.Contains(t, prompt, "Check security")
}

func TestBuildReviewPrompt_EmptyTarget_DefaultsToUncommitted(t *testing.T) {
	prompt := buildReviewPrompt("", ReviewTarget{}, "")
	assert.Contains(t, prompt, "Review the current code changes")
}
