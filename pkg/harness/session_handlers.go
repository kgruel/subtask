package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kgruel/subtask/internal/homedir"
)

// migrateSessionByHandler dispatches session migration to the appropriate handler.
// Known handlers: "none" (or ""), "codex", "claude".
func migrateSessionByHandler(handler, sessionID, oldCwd, newCwd string) error {
	switch handler {
	case "none", "":
		return nil
	case "codex":
		// Codex session IDs are global, no migration needed.
		return nil
	case "claude":
		return migrateClaudeSession(sessionID, oldCwd, newCwd)
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
		return duplicateCodexSession(sessionID, oldCwd, newCwd)
	case "claude":
		return duplicateClaudeSession(sessionID, oldCwd, newCwd)
	default:
		return "", fmt.Errorf("unknown session handler: %q", handler)
	}
}

// ---------------------------------------------------------------------------
// Claude session operations
// ---------------------------------------------------------------------------

// migrateClaudeSession moves a Claude session from oldCwd to newCwd.
func migrateClaudeSession(sessionID, oldCwd, newCwd string) error {
	if sessionID == "" || oldCwd == "" || newCwd == "" {
		return nil
	}
	if filepath.Clean(oldCwd) == filepath.Clean(newCwd) {
		return nil
	}

	home, err := homedir.Dir()
	if err != nil {
		return err
	}

	oldProject := filepath.Join(home, ".claude", "projects", escapeClaudeProjectDir(oldCwd))
	newProject := filepath.Join(home, ".claude", "projects", escapeClaudeProjectDir(newCwd))

	srcJSONL := filepath.Join(oldProject, sessionID+".jsonl")
	dstJSONL := filepath.Join(newProject, sessionID+".jsonl")

	if _, err := os.Stat(srcJSONL); err != nil {
		return fmt.Errorf("claude session not found at %s: %w", srcJSONL, err)
	}
	if err := os.MkdirAll(newProject, 0700); err != nil {
		return err
	}

	if err := copyFileMode(srcJSONL, dstJSONL, 0600); err != nil {
		return err
	}

	// Copy per-session directory if present (tool-results, subagents, etc).
	srcDir := filepath.Join(oldProject, sessionID)
	dstDir := filepath.Join(newProject, sessionID)
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		if err := copyDirRecursive(srcDir, dstDir); err != nil {
			return err
		}
	}

	// Rewrite oldCwd -> newCwd in the copied artifacts.
	if err := replaceAllInFile(dstJSONL, oldCwd, newCwd); err != nil {
		return err
	}
	if st, err := os.Stat(dstDir); err == nil && st.IsDir() {
		if err := replaceAllInDir(dstDir, oldCwd, newCwd); err != nil {
			return err
		}
	}

	// Verify the migrated session doesn't reference oldCwd anymore.
	if has, err := fileContains(dstJSONL, oldCwd); err != nil {
		return err
	} else if has {
		return fmt.Errorf("migrated session still references old cwd")
	}
	if st, err := os.Stat(dstDir); err == nil && st.IsDir() {
		if has, err := dirContains(dstDir, oldCwd); err != nil {
			return err
		} else if has {
			return fmt.Errorf("migrated session dir still references old cwd")
		}
	}

	// Migration semantics: move (delete source after successful copy + rewrite).
	if err := os.Remove(srcJSONL); err != nil && !os.IsNotExist(err) {
		return err
	}
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		if err := os.RemoveAll(srcDir); err != nil {
			return err
		}
	}

	return nil
}

// duplicateClaudeSession creates a duplicate Claude session in newCwd.
func duplicateClaudeSession(sessionID, oldCwd, newCwd string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	if oldCwd == "" || newCwd == "" {
		return "", fmt.Errorf("duplicate session requires both oldCwd and newCwd")
	}
	sameCwd := filepath.Clean(oldCwd) == filepath.Clean(newCwd)

	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}

	oldProject := filepath.Join(home, ".claude", "projects", escapeClaudeProjectDir(oldCwd))
	newProject := filepath.Join(home, ".claude", "projects", escapeClaudeProjectDir(newCwd))

	srcJSONL := filepath.Join(oldProject, sessionID+".jsonl")
	if _, err := os.Stat(srcJSONL); err != nil {
		return "", fmt.Errorf("claude session not found at %s: %w", srcJSONL, err)
	}
	if err := os.MkdirAll(newProject, 0700); err != nil {
		return "", err
	}

	newSessionID, err := newUUIDv4()
	if err != nil {
		return "", err
	}

	dstJSONL := filepath.Join(newProject, newSessionID+".jsonl")
	if err := copyFileMode(srcJSONL, dstJSONL, 0600); err != nil {
		return "", err
	}

	// Copy per-session directory if present (tool-results, subagents, etc).
	srcDir := filepath.Join(oldProject, sessionID)
	dstDir := filepath.Join(newProject, newSessionID)
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		if err := copyDirRecursive(srcDir, dstDir); err != nil {
			return "", err
		}
	}

	// Rewrite oldCwd -> newCwd and sessionID -> newSessionID in the duplicated artifacts.
	if !sameCwd {
		if err := replaceAllInFile(dstJSONL, oldCwd, newCwd); err != nil {
			return "", err
		}
	}
	if err := replaceAllInFile(dstJSONL, sessionID, newSessionID); err != nil {
		return "", err
	}
	if st, err := os.Stat(dstDir); err == nil && st.IsDir() {
		if !sameCwd {
			if err := replaceAllInDir(dstDir, oldCwd, newCwd); err != nil {
				return "", err
			}
		}
		if err := replaceAllInDir(dstDir, sessionID, newSessionID); err != nil {
			return "", err
		}
	}

	// Verify the duplicated session doesn't reference oldCwd anymore.
	if !sameCwd {
		if has, err := fileContains(dstJSONL, oldCwd); err != nil {
			return "", err
		} else if has {
			return "", fmt.Errorf("duplicated session still references old cwd")
		}
		if st, err := os.Stat(dstDir); err == nil && st.IsDir() {
			if has, err := dirContains(dstDir, oldCwd); err != nil {
				return "", err
			} else if has {
				return "", fmt.Errorf("duplicated session dir still references old cwd")
			}
		}
	}

	return newSessionID, nil
}

func escapeClaudeProjectDir(cwd string) string {
	// Observed behavior: replace non-alphanumeric characters with '-', without collapsing.
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Codex session operations
// ---------------------------------------------------------------------------

// duplicateCodexSession creates a duplicate Codex session.
func duplicateCodexSession(sessionID, oldCwd, newCwd string) (string, error) {
	if sessionID == "" {
		return "", nil
	}

	src, err := findCodexSessionFile(sessionID)
	if err != nil {
		return "", err
	}

	newSessionID, err := newUUIDv4()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(src)
	base := filepath.Base(src)
	newBase := strings.Replace(base, sessionID, newSessionID, 1)
	if newBase == base {
		// Unexpected (filename should contain session ID), but fall back to suffixing.
		newBase = strings.TrimSuffix(base, ".jsonl") + "-" + newSessionID + ".jsonl"
	}
	dst := filepath.Join(dir, newBase)

	// Extremely unlikely, but avoid clobbering an existing file.
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("duplicate session destination already exists: %s", dst)
	}

	if err := copyCodexSessionWithNewID(src, dst, sessionID, newSessionID); err != nil {
		return "", err
	}
	return newSessionID, nil
}

func findCodexSessionFile(sessionID string) (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}

	sessionsDir := filepath.Join(home, ".codex", "sessions")

	var found string
	err = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}

func copyCodexSessionWithNewID(src, dst, oldSessionID, newSessionID string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}

	reader := bufio.NewReader(in)
	updated := false

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !updated && bytes.Contains(line, []byte(`"type":"session_meta"`)) {
				trimmed := bytes.TrimSpace(line)
				var ev struct {
					Timestamp string         `json:"timestamp"`
					Type      string         `json:"type"`
					Payload   map[string]any `json:"payload"`
				}
				if err := json.Unmarshal(trimmed, &ev); err == nil && ev.Type == "session_meta" && ev.Payload != nil {
					if id, ok := ev.Payload["id"].(string); ok && id == oldSessionID {
						ev.Payload["id"] = newSessionID
						b, err := json.Marshal(ev)
						if err == nil {
							if _, err := out.Write(b); err != nil {
								out.Close()
								_ = os.Remove(tmp)
								return err
							}
							if len(line) > 0 && line[len(line)-1] == '\n' {
								if _, err := out.Write([]byte("\n")); err != nil {
									out.Close()
									_ = os.Remove(tmp)
									return err
								}
							}
							updated = true
							if readErr == io.EOF {
								break
							}
							if readErr != nil {
								out.Close()
								_ = os.Remove(tmp)
								return readErr
							}
							continue
						}
					}
				}
			}

			if _, err := out.Write(line); err != nil {
				out.Close()
				_ = os.Remove(tmp)
				return err
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			_ = os.Remove(tmp)
			return readErr
		}
	}

	if !updated {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to update codex session_meta id in duplicated session")
	}

	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dst)
}

// extractWorkspaceID extracts the workspace number from a workspace path.
// e.g., "/Users/foo/.subtask/workspaces/-Users-foo-code-project--2" -> 2
func extractWorkspaceID(workspacePath string) int {
	base := filepath.Base(workspacePath)
	if idx := strings.LastIndex(base, "--"); idx != -1 {
		if id, err := strconv.Atoi(base[idx+2:]); err == nil {
			return id
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// File helpers (used by Claude and Codex session operations)
// ---------------------------------------------------------------------------

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dst)
}

func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFileMode(path, target, info.Mode())
	})
}

func replaceAllInFile(path, old, new string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := strings.ReplaceAll(string(data), old, new)
	if updated == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0600)
}

func replaceAllInDir(dir, old, new string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Best-effort: only rewrite reasonably small files.
		if info, err := d.Info(); err == nil {
			if info.Size() > 5*1024*1024 {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		s := string(b)
		if !strings.Contains(s, old) {
			return nil
		}
		s = strings.ReplaceAll(s, old, new)
		_ = os.WriteFile(path, []byte(s), 0600)
		return nil
	})
}

func fileContains(path, substr string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(b), substr), nil
}

func dirContains(dir, substr string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > 5*1024*1024 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), substr) {
			found = true
		}
		return nil
	})
	return found, err
}
