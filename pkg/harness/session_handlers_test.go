package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionHandler_None_Migrate(t *testing.T) {
	err := migrateSessionByHandler("none", "sess-1", "/old", "/new")
	require.NoError(t, err)
}

func TestSessionHandler_None_MigrateEmpty(t *testing.T) {
	// Empty string should behave like "none".
	err := migrateSessionByHandler("", "sess-1", "/old", "/new")
	require.NoError(t, err)
}

func TestSessionHandler_None_Duplicate(t *testing.T) {
	_, err := duplicateSessionByHandler("none", "sess-1", "/old", "/new")
	require.Error(t, err)
}

func TestSessionHandler_None_DuplicateEmpty(t *testing.T) {
	_, err := duplicateSessionByHandler("", "sess-1", "/old", "/new")
	require.Error(t, err)
}

func TestSessionHandler_Unknown_Migrate(t *testing.T) {
	err := migrateSessionByHandler("nonexistent", "sess-1", "/old", "/new")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestSessionHandler_Unknown_Duplicate(t *testing.T) {
	_, err := duplicateSessionByHandler("nonexistent", "sess-1", "/old", "/new")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}
