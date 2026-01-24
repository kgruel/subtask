package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatRepoStatusWarning(t *testing.T) {
	require.Equal(t, "", FormatRepoStatusWarning("main", nil))

	require.Equal(t,
		"",
		FormatRepoStatusWarning("main", &RepoStatus{}),
	)

	require.Equal(t,
		"Note: This branch conflicts with main in: a.txt, b.txt. Consider running `git merge main` to resolve.",
		FormatRepoStatusWarning("main", &RepoStatus{
			ConflictFiles: []string{"a.txt", "b.txt"},
		}),
	)
}
