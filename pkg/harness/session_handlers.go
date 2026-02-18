package harness

import "fmt"

// migrateSessionByHandler dispatches session migration to the appropriate handler.
// Known handlers: "none" (or ""), "codex", "claude".
func migrateSessionByHandler(handler, sessionID, oldCwd, newCwd string) error {
	switch handler {
	case "none", "":
		return nil
	case "codex":
		h := &CodexHarness{}
		return h.MigrateSession(sessionID, oldCwd, newCwd)
	case "claude":
		h := &ClaudeHarness{}
		return h.MigrateSession(sessionID, oldCwd, newCwd)
	default:
		return fmt.Errorf("unknown session handler: %q", handler)
	}
}

// duplicateSessionByHandler dispatches session duplication to the appropriate handler.
// Known handlers: "none" (or ""), "codex", "claude".
func duplicateSessionByHandler(handler, sessionID, oldCwd, newCwd string) (string, error) {
	switch handler {
	case "none", "":
		return "", fmt.Errorf("session duplication not supported (session_handler=%q)", handler)
	case "codex":
		h := &CodexHarness{}
		return h.DuplicateSession(sessionID, oldCwd, newCwd)
	case "claude":
		h := &ClaudeHarness{}
		return h.DuplicateSession(sessionID, oldCwd, newCwd)
	default:
		return "", fmt.Errorf("unknown session handler: %q", handler)
	}
}
