package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatRepoStatusWarning(t *testing.T) {
	require.Equal(t, "", FormatRepoStatusWarning("main", nil))

	require.Equal(t,
		"Note: main is 3 commits ahead of this task.",
		FormatRepoStatusWarning("main", &RepoStatus{CommitsBehind: 3}),
	)

	require.Equal(t,
		"Note: main is 1 commit ahead of this task.\n"+
			"Note: This branch conflicts with main in: a.txt, b.txt. Consider running `git merge main` to resolve.",
		FormatRepoStatusWarning("main", &RepoStatus{
			CommitsBehind: 1,
			ConflictFiles: []string{"a.txt", "b.txt"},
		}),
	)
}
