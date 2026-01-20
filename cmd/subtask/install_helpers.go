package main

import (
	"fmt"
	"os"

	"github.com/zippoxer/subtask/internal/homedir"
	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/install"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/workspace"
)

func parseInstallScope(s string) (install.Scope, error) {
	switch s {
	case "", "user":
		return install.ScopeUser, nil
	case "project":
		return install.ScopeProject, nil
	default:
		return "", fmt.Errorf("invalid scope %q (expected user|project)", s)
	}
}

func projectRootFromCwd() (root string, inGit bool, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}

	insideWorkTree, err := git.Output(cwd, "rev-parse", "--is-inside-work-tree")
	if err == nil && insideWorkTree == "true" {
		top, err := git.Output(cwd, "rev-parse", "--show-toplevel")
		if err == nil && top != "" {
			return top, true, nil
		}
		return cwd, true, nil
	}

	return cwd, false, nil
}

func baseDirForScope(scope install.Scope) (baseDir string, inGit bool, err error) {
	switch scope {
	case install.ScopeUser:
		homeDir, err := homedir.Dir()
		if err != nil {
			return "", false, err
		}
		return homeDir, false, nil
	case install.ScopeProject:
		return projectRootFromCwd()
	default:
		return "", false, fmt.Errorf("invalid scope %q", scope)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func initSubtaskDefaults(repoRoot string) error {
	if repoRoot == "" {
		return fmt.Errorf("invalid repo root")
	}

	prev, _ := os.Getwd()
	if err := os.Chdir(repoRoot); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(prev) }()

	if _, err := os.Stat(task.ConfigPath()); err == nil {
		return nil
	}

	codexAvailable := isCommandAvailable("codex")
	claudeAvailable := isCommandAvailable("claude")
	opencodeAvailable := isCommandAvailable("opencode")
	if !codexAvailable && !claudeAvailable && !opencodeAvailable {
		return fmt.Errorf("no worker harness available\n\nInstall one of:\n  - Codex CLI: https://github.com/openai/codex\n  - Claude Code CLI: https://claude.com/claude-code\n  - OpenCode CLI: https://github.com/anomalyco/opencode")
	}

	h := "codex"
	if !codexAvailable {
		if claudeAvailable {
			h = "claude"
		} else {
			h = "opencode"
		}
	}

	model := "gpt-5.2"
	reasoning := "xhigh"
	if h == "claude" {
		model = "claude-opus-4-5-20251101"
		reasoning = ""
	}
	if h == "opencode" {
		model = ""
		reasoning = ""
	}

	cfg := &workspace.Config{
		Harness:       h,
		MaxWorkspaces: workspace.DefaultMaxWorkspaces,
		Options:       make(map[string]any),
	}
	if model != "" {
		cfg.Options["model"] = model
	}
	if reasoning != "" {
		cfg.Options["reasoning"] = reasoning
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := ensureGitignore(repoRoot); err != nil {
		// best effort
	}

	// Warm harness discovery for better UX on first run.
	_ = harness.CanResolveCLI(cfg.Harness)

	return nil
}
