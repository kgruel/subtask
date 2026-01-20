package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zippoxer/subtask/internal/homedir"
)

type cliSpec struct {
	Exec       string
	PrefixArgs []string
}

// CanResolveCLI returns true if subtask can likely invoke the given CLI name on this machine,
// including common install locations.
//
// Note: this is intentionally "side-effect free" (it does not invoke a shell),
// so it may return false even if the command would be available via a shell alias.
func CanResolveCLI(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	_, ok := findCLIInCommonLocations(name)
	return ok
}

func cliSpecFromOptions(opts map[string]any, defaultExec string) cliSpec {
	// options.cli can be either:
	//   - string: executable name/path
	//   - array:  ["executable", "fixed", "args"]
	//
	// Anything else falls back to defaultExec.
	if opts != nil {
		if v, ok := opts["cli"]; ok {
			switch vv := v.(type) {
			case string:
				if strings.TrimSpace(vv) != "" {
					return cliSpec{Exec: vv}
				}
			case []any:
				var parts []string
				for _, item := range vv {
					s, ok := item.(string)
					if !ok {
						continue
					}
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					parts = append(parts, s)
				}
				if len(parts) > 0 {
					return cliSpec{Exec: parts[0], PrefixArgs: parts[1:]}
				}
			case []string:
				var parts []string
				for _, s := range vv {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					parts = append(parts, s)
				}
				if len(parts) > 0 {
					return cliSpec{Exec: parts[0], PrefixArgs: parts[1:]}
				}
			}
		}
	}
	return cliSpec{Exec: defaultExec}
}

func commandForCLI(ctx context.Context, spec cliSpec, args []string) (*exec.Cmd, error) {
	execName := strings.TrimSpace(spec.Exec)
	if execName == "" {
		return nil, fmt.Errorf("empty cli executable")
	}

	allArgs := append(append([]string{}, spec.PrefixArgs...), args...)

	// 1) Standard PATH resolution.
	if path, err := exec.LookPath(execName); err == nil {
		cmd := exec.CommandContext(ctx, path, allArgs...)
		configureCmdCancellation(cmd)
		return cmd, nil
	}

	// 2) Known/common install locations (especially when users add an alias instead of PATH).
	if path, ok := findCLIInCommonLocations(execName); ok {
		cmd := exec.CommandContext(ctx, path, allArgs...)
		configureCmdCancellation(cmd)
		return cmd, nil
	}

	// 3) Shell fallback (aliases/functions/rc-injected PATH).
	if isSafeShellWord(execName) && canFindViaShell(ctx, execName) {
		shell, shellArgs, err := shellCommand(execName, allArgs)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, shell, shellArgs...)
		configureCmdCancellation(cmd)
		return cmd, nil
	}

	candidates := commonCandidatePaths(execName)
	var b strings.Builder
	fmt.Fprintf(&b, "%q not found.\n", execName)
	b.WriteString("\nSearched:\n")
	b.WriteString("- PATH\n")
	if len(candidates) > 0 {
		for _, p := range candidates {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}
	if runtime.GOOS != "windows" {
		b.WriteString("- Your shell (for aliases/functions)\n")
	}
	b.WriteString("\nFix:\n")
	b.WriteString("- Install the CLI (or ensure it's on PATH)\n")
	b.WriteString("- Or set `.subtask/config.json` harness `options.cli` to an executable path (or [\"exe\", \"fixed\", \"args\"])\n")
	return nil, fmt.Errorf("%s", strings.TrimSpace(b.String()))
}

func findCLIInCommonLocations(execName string) (string, bool) {
	for _, p := range commonCandidatePaths(execName) {
		if isExecutableFile(p) {
			return p, true
		}
	}
	return "", false
}

func commonCandidatePaths(execName string) []string {
	var candidates []string

	home, err := homedir.Dir()
	if err == nil && home != "" {
		switch execName {
		case "claude":
			// Claude Code's installer/migration can set up a shell alias to this path.
			// (Users then won't have "claude" in PATH.)
			candidates = append(candidates,
				filepath.Join(home, ".claude", "local", "claude"),
				filepath.Join(home, ".claude", "local", "bin", "claude"),
				filepath.Join(home, ".local", "bin", "claude"),
			)
		case "codex":
			candidates = append(candidates,
				filepath.Join(home, ".local", "bin", "codex"),
				filepath.Join(home, ".cargo", "bin", "codex"),
			)
		case "opencode":
			// OpenCode's installer can place the CLI here without adding it to PATH.
			candidates = append(candidates,
				filepath.Join(home, ".opencode", "bin", "opencode"),
			)
		}

		// Generic user-local bins that are frequently on PATH only in interactive shells.
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", execName),
			filepath.Join(home, "bin", execName),
			filepath.Join(home, ".cargo", "bin", execName),
		)
	}

	// Homebrew defaults (macOS) and common Unix locations (also relevant for GUI-launched apps with minimal PATH).
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			filepath.Join("/opt", "homebrew", "bin", execName),
			filepath.Join("/usr", "local", "bin", execName),
		)
	} else {
		candidates = append(candidates, filepath.Join("/usr", "local", "bin", execName))
	}

	return dedupeStrings(candidates)
}

func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return st.Mode()&0o111 != 0
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func canFindViaShell(ctx context.Context, execName string) bool {
	if runtime.GOOS == "windows" {
		return false
	}

	shell, shellArgs, err := shellCheckCommand(execName)
	if err != nil {
		return false
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, shell, shellArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	configureCmdCancellation(cmd)
	return cmd.Run() == nil
}

func shellCommand(execName string, args []string) (string, []string, error) {
	shell, kind, err := preferredShell()
	if err != nil {
		return "", nil, err
	}

	switch kind {
	case shellKindFish:
		shellArgs := []string{"-l", "-i", "-c", execName + " $argv", "--"}
		shellArgs = append(shellArgs, args...)
		return shell, shellArgs, nil
	case shellKindPOSIX:
		shellArgs := []string{"-l", "-i", "-c", execName + ` "$@"`, "--"}
		shellArgs = append(shellArgs, args...)
		return shell, shellArgs, nil
	default:
		return "", nil, fmt.Errorf("unsupported shell kind: %v", kind)
	}
}

func shellCheckCommand(execName string) (string, []string, error) {
	shell, kind, err := preferredShell()
	if err != nil {
		return "", nil, err
	}

	switch kind {
	case shellKindFish:
		// fish: `type -q` checks whether a command/function exists.
		return shell, []string{"-l", "-i", "-c", "type -q " + execName}, nil
	case shellKindPOSIX:
		// POSIX-ish shells: `type` detects aliases, functions, and PATH commands.
		return shell, []string{"-l", "-i", "-c", "type " + execName + " >/dev/null 2>&1"}, nil
	default:
		return "", nil, fmt.Errorf("unsupported shell kind: %v", kind)
	}
}

type shellKind int

const (
	shellKindPOSIX shellKind = iota
	shellKindFish
)

func preferredShell() (string, shellKind, error) {
	if v := strings.TrimSpace(os.Getenv("SHELL")); v != "" {
		if path, err := resolveShellPath(v); err == nil {
			return path, classifyShell(path), nil
		}
	}

	// Reasonable fallbacks (Unix only).
	if runtime.GOOS == "windows" {
		return "", 0, fmt.Errorf("shell resolution not supported on windows")
	}
	for _, name := range []string{"zsh", "bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, classifyShell(p), nil
		}
	}
	return "", 0, fmt.Errorf("no usable shell found (SHELL is unset and zsh/bash/sh not found in PATH)")
}

func resolveShellPath(shell string) (string, error) {
	// Support both absolute paths (typical) and bare names.
	if filepath.IsAbs(shell) {
		if isExecutableFile(shell) {
			return shell, nil
		}
		return "", fmt.Errorf("shell %q is not executable", shell)
	}
	p, err := exec.LookPath(shell)
	if err != nil {
		return "", err
	}
	if !isExecutableFile(p) {
		return "", fmt.Errorf("shell %q is not executable", p)
	}
	return p, nil
}

func classifyShell(shellPath string) shellKind {
	base := strings.ToLower(filepath.Base(shellPath))
	if base == "fish" {
		return shellKindFish
	}
	// Default to POSIX-ish behavior for bash/zsh/sh/dash/ksh/etc.
	return shellKindPOSIX
}

func isSafeShellWord(s string) bool {
	// Must be safe to embed as the first token of a shell command.
	// (We intentionally don't attempt to support arbitrary strings here.)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.ContainsAny(s, `/\`) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '+':
		default:
			return false
		}
	}
	return true
}
