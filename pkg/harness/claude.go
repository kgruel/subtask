package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zippoxer/subtask/internal/homedir"
)

// ClaudeHarness implements Harness for the Claude Code CLI.
type ClaudeHarness struct {
	cli            cliSpec
	Model          string
	PermissionMode string // default: bypassPermissions
	Tools          string // optional, maps to --tools
}

// Run executes Claude with the given prompt. Blocks until completion.
func (c *ClaudeHarness) Run(ctx context.Context, cwd, prompt, continueFrom string, cb Callbacks) (*Result, error) {
	args := []string{
		"--print",
		"--verbose",
		"--output-format=stream-json",
		"--permission-mode", c.permissionMode(),
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Tools != "" {
		args = append(args, "--tools", c.Tools)
	}

	if continueFrom != "" {
		args = append(args, "--resume", continueFrom)
	}
	args = append(args, prompt)

	cmd, err := commandForCLI(ctx, c.effectiveCLI(), args)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	result := &Result{}

	// Collect stderr concurrently for better error messages.
	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	parseErr := parseClaudeStream(stdout, result, cb)

	cmdErr := cmd.Wait()
	<-stderrDone

	if parseErr != nil && result.Error == "" {
		result.Error = parseErr.Error()
	}
	if cmdErr != nil && result.Error == "" {
		result.Error = strings.TrimSpace(stderrBuf.String())
		if result.Error == "" {
			result.Error = cmdErr.Error()
		}
		return result, fmt.Errorf("claude failed: %w", cmdErr)
	}
	if result.Error != "" {
		return result, fmt.Errorf("claude error: %s", result.Error)
	}

	return result, nil
}

// Review runs a code review using the standard Run infrastructure.
func (c *ClaudeHarness) Review(cwd string, target ReviewTarget, instructions string) (string, error) {
	prompt := buildReviewPrompt(cwd, target, instructions)
	result, err := c.Run(context.Background(), cwd, prompt, "", Callbacks{})
	if err != nil {
		return "", err
	}
	return result.Reply, nil
}

func (c *ClaudeHarness) effectiveCLI() cliSpec {
	if strings.TrimSpace(c.cli.Exec) == "" {
		return cliSpec{Exec: "claude"}
	}
	return c.cli
}

func (c *ClaudeHarness) MigrateSession(sessionID, oldCwd, newCwd string) error {
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

	if err := copyFile(srcJSONL, dstJSONL, 0600); err != nil {
		return err
	}

	// Copy per-session directory if present (tool-results, subagents, etc).
	srcDir := filepath.Join(oldProject, sessionID)
	dstDir := filepath.Join(newProject, sessionID)
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		if err := copyDir(srcDir, dstDir); err != nil {
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

func (c *ClaudeHarness) permissionMode() string {
	if c.PermissionMode != "" {
		return c.PermissionMode
	}
	return "bypassPermissions"
}

func (c *ClaudeHarness) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
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
	if err := copyFile(srcJSONL, dstJSONL, 0600); err != nil {
		return "", err
	}

	// Copy per-session directory if present (tool-results, subagents, etc).
	srcDir := filepath.Join(oldProject, sessionID)
	dstDir := filepath.Join(newProject, newSessionID)
	if st, err := os.Stat(srcDir); err == nil && st.IsDir() {
		if err := copyDir(srcDir, dstDir); err != nil {
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

func copyFile(src, dst string, mode os.FileMode) error {
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

func copyDir(src, dst string) error {
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
		return copyFile(path, target, info.Mode())
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
