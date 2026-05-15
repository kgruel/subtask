package workspace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnknownConfigKeys(t *testing.T) {
	t.Run("no_unknown_keys", func(t *testing.T) {
		data := []byte(`{"adapter":"claude","model":"claude-opus-4-5","max_workspaces":5}`)
		require.Empty(t, unknownConfigKeys(data))
	})

	t.Run("legacy_keys_are_known", func(t *testing.T) {
		data := []byte(`{"harness":"codex","options":{"model":"o3"}}`)
		require.Empty(t, unknownConfigKeys(data))
	})

	t.Run("unknown_key_returned", func(t *testing.T) {
		data := []byte(`{"adapter":"claude","types":{"foo":"bar"},"workflows":"old"}`)
		got := unknownConfigKeys(data)
		require.Equal(t, []string{"types", "workflows"}, got)
	})

	t.Run("non_object_returns_nil", func(t *testing.T) {
		require.Nil(t, unknownConfigKeys([]byte(`"not an object"`)))
		require.Nil(t, unknownConfigKeys([]byte(`null`)))
		require.Nil(t, unknownConfigKeys([]byte(`[]`)))
	})

	t.Run("empty_object", func(t *testing.T) {
		require.Empty(t, unknownConfigKeys([]byte(`{}`)))
	})
}
