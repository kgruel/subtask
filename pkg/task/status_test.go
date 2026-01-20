package task

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeWorkerStatus_LegacyValues(t *testing.T) {
	require.Equal(t, WorkerStatusNotStarted, ParseWorkerStatus(""))
	require.Equal(t, WorkerStatusNotStarted, ParseWorkerStatus("idle"))
	require.Equal(t, WorkerStatusRunning, ParseWorkerStatus("running"))
	require.Equal(t, WorkerStatusRunning, ParseWorkerStatus("working"))
	require.Equal(t, WorkerStatusReplied, ParseWorkerStatus("replied"))
	require.Equal(t, WorkerStatusError, ParseWorkerStatus("error"))
}

func TestUserStatusFor_LegacyRunning(t *testing.T) {
	require.Equal(t, UserStatusRunning, UserStatusFor(TaskStatusOpen, WorkerStatus("running")))
	require.Equal(t, UserStatusRunning, UserStatusFor(TaskStatusOpen, WorkerStatus("working")))
}
