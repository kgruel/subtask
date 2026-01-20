package harness

import (
	"context"
	"errors"
	"sync"
	"time"
)

// MockHarness implements Harness for testing.
type MockHarness struct {
	mu sync.Mutex

	// Configuration for Run behavior
	RunResult   *Result
	RunError    error
	ToolCallN   int           // Number of times to call onToolCall
	ToolCallGap time.Duration // Delay between tool calls

	// Configuration for Review behavior
	ReviewResult string
	ReviewError  error

	// Configuration for DuplicateSession behavior
	DuplicateResult string
	DuplicateError  error

	// Call tracking for assertions
	RunCalls       []RunCall
	MigrateCalls   []MigrateCall
	DuplicateCalls []DuplicateCall
	ReviewCalls    []ReviewCall
}

// RunCall records parameters from a Run invocation.
type RunCall struct {
	CWD          string
	Prompt       string
	ContinueFrom string
	ToolCalls    int
	Timestamp    time.Time
}

// ReviewCall records parameters from a Review invocation.
type ReviewCall struct {
	CWD          string
	Target       ReviewTarget
	Instructions string
	Timestamp    time.Time
}

// MigrateCall records parameters from a MigrateSession invocation.
type MigrateCall struct {
	SessionID string
	OldCWD    string
	NewCWD    string
	Timestamp time.Time
}

// DuplicateCall records parameters from a DuplicateSession invocation.
type DuplicateCall struct {
	SessionID string
	OldCWD    string
	NewCWD    string
	Timestamp time.Time
}

// NewMockHarness creates a mock with default successful behavior.
func NewMockHarness() *MockHarness {
	return &MockHarness{
		RunResult: &Result{
			Reply:           "Mock response from harness",
			SessionID:       "mock-session-12345678",
			PromptDelivered: true,
			AgentReplied:    true,
		},
		DuplicateResult: "mock-session-dup-12345678",
		ReviewResult:    "No issues found.",
	}
}

// Run implements Harness.Run with configurable behavior.
func (m *MockHarness) Run(ctx context.Context, cwd, prompt, continueFrom string, cb Callbacks) (*Result, error) {
	m.mu.Lock()
	call := RunCall{
		CWD:          cwd,
		Prompt:       prompt,
		ContinueFrom: continueFrom,
		Timestamp:    time.Now(),
	}
	m.RunCalls = append(m.RunCalls, call)

	result := m.RunResult
	err := m.RunError
	toolCallN := m.ToolCallN
	toolCallGap := m.ToolCallGap
	m.mu.Unlock()

	// Notify session start
	if cb.OnSessionStart != nil && result != nil && result.SessionID != "" {
		cb.OnSessionStart(result.SessionID)
	}

	// Simulate tool calls
	if cb.OnToolCall != nil && toolCallN > 0 {
		for i := 0; i < toolCallN; i++ {
			if toolCallGap > 0 && i > 0 {
				time.Sleep(toolCallGap)
			}
			cb.OnToolCall(time.Now())
		}
		m.mu.Lock()
		m.RunCalls[len(m.RunCalls)-1].ToolCalls = toolCallN
		m.mu.Unlock()
	}

	if err != nil {
		return nil, err
	}
	return result, nil
}

// Review implements Harness.Review.
func (m *MockHarness) Review(cwd string, target ReviewTarget, instructions string) (string, error) {
	m.mu.Lock()
	m.ReviewCalls = append(m.ReviewCalls, ReviewCall{
		CWD:          cwd,
		Target:       target,
		Instructions: instructions,
		Timestamp:    time.Now(),
	})
	result := m.ReviewResult
	err := m.ReviewError
	m.mu.Unlock()

	if err != nil {
		return "", err
	}
	return result, nil
}

// MigrateSession implements Harness.MigrateSession.
func (m *MockHarness) MigrateSession(sessionID, oldCwd, newCwd string) error {
	m.mu.Lock()
	m.MigrateCalls = append(m.MigrateCalls, MigrateCall{
		SessionID: sessionID,
		OldCWD:    oldCwd,
		NewCWD:    newCwd,
		Timestamp: time.Now(),
	})
	m.mu.Unlock()
	return nil
}

// DuplicateSession implements Harness.DuplicateSession.
func (m *MockHarness) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
	m.mu.Lock()
	m.DuplicateCalls = append(m.DuplicateCalls, DuplicateCall{
		SessionID: sessionID,
		OldCWD:    oldCwd,
		NewCWD:    newCwd,
		Timestamp: time.Now(),
	})
	result := m.DuplicateResult
	err := m.DuplicateError
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	return result, nil
}

// Reset clears all call tracking.
func (m *MockHarness) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunCalls = nil
	m.MigrateCalls = nil
	m.DuplicateCalls = nil
	m.ReviewCalls = nil
}

// RunCallCount returns number of Run invocations.
func (m *MockHarness) RunCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.RunCalls)
}

// LastRunCall returns the most recent Run call, or nil if none.
func (m *MockHarness) LastRunCall() *RunCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.RunCalls) == 0 {
		return nil
	}
	call := m.RunCalls[len(m.RunCalls)-1]
	return &call
}

// WithError configures the mock to return an error.
func (m *MockHarness) WithError(err error) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunError = err
	return m
}

// WithResult configures the mock to return specific results.
func (m *MockHarness) WithResult(reply, sessionID string) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunResult = &Result{
		Reply:           reply,
		SessionID:       sessionID,
		PromptDelivered: true,
		AgentReplied:    true,
	}
	return m
}

// WithPartialResult simulates a partial execution (prompt delivered but no reply).
func (m *MockHarness) WithPartialResult(sessionID, errorMsg string) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunResult = &Result{
		SessionID:       sessionID,
		PromptDelivered: true,
		AgentReplied:    false,
		Error:           errorMsg,
	}
	return m
}

// WithFailedStart simulates a failure before session started.
func (m *MockHarness) WithFailedStart(errorMsg string) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunResult = &Result{
		PromptDelivered: false,
		AgentReplied:    false,
		Error:           errorMsg,
	}
	return m
}

// WithToolCalls configures simulation of N tool calls.
func (m *MockHarness) WithToolCalls(n int) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ToolCallN = n
	return m
}

// WithReviewResult configures the review response.
func (m *MockHarness) WithReviewResult(result string) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReviewResult = result
	return m
}

// WithReviewError configures review to return an error.
func (m *MockHarness) WithReviewError(err error) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReviewError = err
	return m
}

// WithDuplicateResult configures DuplicateSession to return a specific new session ID.
func (m *MockHarness) WithDuplicateResult(sessionID string) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DuplicateResult = sessionID
	return m
}

// WithDuplicateError configures DuplicateSession to return an error.
func (m *MockHarness) WithDuplicateError(err error) *MockHarness {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DuplicateError = err
	return m
}

// Common mock errors for testing
var (
	ErrMockTimeout = errors.New("mock harness: operation timed out")
	ErrMockFailed  = errors.New("mock harness: execution failed")
)
