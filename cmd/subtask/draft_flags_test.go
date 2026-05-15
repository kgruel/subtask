package main

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDraftCmd_AgentAndRoutineFlagHelp_MentionMutualExclusion(t *testing.T) {
	rt := reflect.TypeFor[DraftCmd]()

	agent, ok := rt.FieldByName("Agent")
	require.True(t, ok)
	require.Contains(t, agent.Tag.Get("help"), "--routine",
		"--agent flag help should mention --routine as mutually exclusive")

	routine, ok := rt.FieldByName("Routine")
	require.True(t, ok)
	require.Contains(t, routine.Tag.Get("help"), "--agent",
		"--routine flag help should mention --agent as mutually exclusive")
}
