package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/workspace"
)

// InitCmd implements 'subtask init'.
type InitCmd struct {
	Workspaces int    `short:"n" default:"20" help:"Maximum number of workspaces (created on demand)"`
	Harness    string `default:"codex" help:"Worker harness (codex|claude|opencode)"`
	Force      bool   `short:"f" help:"Force re-init, overwriting existing config"`
}

var (
	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))

	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("247"))
)

// Run executes the init command.
func (c *InitCmd) Run() error {
	// Get current directory as project root
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// For init, always check/create in cwd, don't search ancestors
	localSubtaskDir := filepath.Join(cwd, ".subtask")
	localConfigPath := filepath.Join(localSubtaskDir, "config.json")

	// Check if already initialized in this directory
	if _, err := os.Stat(localConfigPath); err == nil && !c.Force {
		return fmt.Errorf("already initialized\n\nConfig exists: %s\nUse --force to reinitialize", localConfigPath)
	}

	insideWorkTree, err := git.Output(cwd, "rev-parse", "--is-inside-work-tree")
	if err != nil || insideWorkTree != "true" {
		return fmt.Errorf("Not a git repository. Run 'git init' first or cd to an existing repo.")
	}

	// Check which harnesses are available
	codexAvailable := isCommandAvailable("codex")
	claudeAvailable := isCommandAvailable("claude")
	opencodeAvailable := isCommandAvailable("opencode")

	if !codexAvailable && !claudeAvailable && !opencodeAvailable {
		return fmt.Errorf("no worker harness available\n\nInstall one of:\n  - Codex CLI: https://github.com/openai/codex\n  - Claude Code CLI: https://claude.com/claude-code\n  - OpenCode CLI: https://github.com/anomalyco/opencode")
	}

	// With --force, confirm before overwriting config.
	if c.Force {
		if _, err := os.Stat(localConfigPath); err == nil {
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("  ⚠ Warning"))
			fmt.Println("    • existing config will be overwritten")
			fmt.Println()

			var confirm bool
			confirmForm := huh.NewForm(huh.NewGroup(
				huh.NewConfirm().
					Title("Continue with --force?").
					Description("This cannot be undone").
					Value(&confirm),
			)).WithTheme(huh.ThemeCharm())

			if err := confirmForm.Run(); err != nil || !confirm {
				return fmt.Errorf("cancelled")
			}
		}
	}

	// Form values
	numWorkspaces := c.Workspaces

	// Validate harness is a supported value
	validHarnesses := map[string]bool{"codex": true, "claude": true, "opencode": true}
	if !validHarnesses[c.Harness] {
		return fmt.Errorf("invalid harness %q\n\nSupported harnesses: codex, claude, opencode", c.Harness)
	}

	harness := c.Harness
	// Fall back if requested harness isn't available
	if !isCommandAvailable(harness) {
		if codexAvailable {
			harness = "codex"
		} else if claudeAvailable {
			harness = "claude"
		} else {
			harness = "opencode"
		}
	}
	model := "gpt-5.2"
	if harness == "claude" {
		model = "claude-opus-4-5-20251101"
	}
	if harness == "opencode" {
		model = ""
	}
	reasoning := "xhigh"
	if harness != "codex" {
		reasoning = ""
	}

	// Determine steps based on what's available
	// Steps: 0=harness (if multiple), 1=model, 2=reasoning (if codex), 3=workspaces
	firstStep := 0
	available := 0
	if codexAvailable {
		available++
	}
	if claudeAvailable {
		available++
	}
	if opencodeAvailable {
		available++
	}
	if available <= 1 {
		firstStep = 1 // skip harness selection
	}

	step := firstStep
	for {
		// Clear screen and show header + previous answers
		fmt.Print("\033[H\033[2J")
		fmt.Println()
		fmt.Println("  " + lipgloss.NewStyle().Bold(true).Render("Subtask Setup"))
		fmt.Println(subtleStyle.Render("  Configure parallel workers for your project"))
		fmt.Println()

		// Show answered questions above current one
		if step > 0 && firstStep == 0 {
			fmt.Printf("  Harness:   %s\n", harness)
		}
		if step > 1 && model != "" {
			fmt.Printf("  Model:     %s\n", model)
		}
		if step > 2 && harness == "codex" {
			fmt.Printf("  Reasoning: %s\n", reasoning)
		}
		if step > firstStep {
			fmt.Println()
		}

		// Determine current question
		var form *huh.Form
		switch step {
		case 0: // Harness
			var opts []huh.Option[string]
			if codexAvailable {
				opts = append(opts, huh.NewOption("Codex (recommended)", "codex"))
			}
			if claudeAvailable {
				opts = append(opts, huh.NewOption("Claude Code", "claude"))
			}
			if opencodeAvailable {
				opts = append(opts, huh.NewOption("OpenCode", "opencode"))
			}
			form = huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Worker").
					Description("Which CLI runs your tasks behind the scenes").
					Options(opts...).
					Value(&harness),
			))

		case 1: // Model (options depend on harness)
			if harness == "codex" {
				opts := []huh.Option[string]{
					huh.NewOption("gpt-5.2 (recommended)", "gpt-5.2"),
					huh.NewOption("gpt-5.2-codex", "gpt-5.2-codex"),
				}
				form = huh.NewForm(huh.NewGroup(
					huh.NewSelect[string]().
						Title("Model").
						Options(opts...).
						Value(&model),
				))
			} else if harness == "claude" {
				opts := []huh.Option[string]{
					huh.NewOption("Claude Opus (recommended)", "claude-opus-4-5-20251101"),
					huh.NewOption("Claude Sonnet", "claude-sonnet-4-20250514"),
				}
				form = huh.NewForm(huh.NewGroup(
					huh.NewSelect[string]().
						Title("Model").
						Options(opts...).
						Value(&model),
				))
			} else {
				form = huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Model (optional)").
						Description("Leave blank to use OpenCode defaults; use provider/model to override.").
						Placeholder("provider/model").
						Value(&model),
				))
			}

		case 2: // Reasoning (Codex only)
			if harness != "codex" {
				step++
				continue
			}
			form = huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Reasoning").
					Options(
						huh.NewOption("Extra High (recommended)", "xhigh"),
						huh.NewOption("High", "high"),
						huh.NewOption("Medium", "medium"),
						huh.NewOption("Low", "low"),
					).
					Value(&reasoning),
			))

		case 3: // Workspaces
			form = huh.NewForm(huh.NewGroup(
				huh.NewSelect[int]().
					Title("Max workspaces").
					Options(
						huh.NewOption("5", 5),
						huh.NewOption("10", 10),
						huh.NewOption("20 (recommended)", 20),
						huh.NewOption("50", 50),
					).
					Value(&numWorkspaces),
			))
		}

		if step > 3 {
			break
		}

		// Configure form - esc/ctrl+c trigger abort, we catch it to go back (or cancel on first)
		km := huh.NewDefaultKeyMap()
		km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "back"))
		km.Select.Filter = key.NewBinding(key.WithDisabled()) // disable "/" filter
		form = form.WithKeyMap(km).WithTheme(huh.ThemeCharm()).WithShowHelp(true)

		err := form.Run()
		if err == huh.ErrUserAborted {
			if step == firstStep {
				return fmt.Errorf("setup cancelled")
			}
			// Go back
			step--
			if step == 2 && harness != "codex" {
				step-- // skip reasoning when going back for claude
			}
			continue
		}
		if err != nil {
			break // non-interactive, use defaults
		}

		// Reset dependent values when harness changes
		if step == 0 {
			if harness == "codex" {
				model = "gpt-5.2"
				reasoning = "xhigh"
			} else if harness == "claude" {
				model = "claude-opus-4-5-20251101"
				reasoning = ""
			} else {
				model = ""
				reasoning = ""
			}
		}

		step++
	}

	// Final validation - ensure selected harness is available
	if harness == "codex" && !codexAvailable {
		return fmt.Errorf("codex CLI not found\n\nInstall it from: https://github.com/openai/codex")
	}
	if harness == "claude" && !claudeAvailable {
		return fmt.Errorf("claude CLI not found\n\nInstall it from: https://claude.com/claude-code")
	}
	if harness == "opencode" && !opencodeAvailable {
		return fmt.Errorf("opencode CLI not found\n\nInstall it from: https://github.com/anomalyco/opencode")
	}

	// Create config (worktrees are created on demand).
	cfg := &workspace.Config{
		Harness:       harness,
		MaxWorkspaces: numWorkspaces,
		Options:       make(map[string]any),
	}

	// Add harness-specific options
	if model != "" {
		cfg.Options["model"] = model
	}
	if reasoning != "" {
		cfg.Options["reasoning"] = reasoning
	}

	// Save config to local directory (not ancestor)
	if err := os.MkdirAll(localSubtaskDir, 0755); err != nil {
		return fmt.Errorf("failed to create .subtask directory: %w", err)
	}
	if cfg.MaxWorkspaces <= 0 {
		cfg.MaxWorkspaces = workspace.DefaultMaxWorkspaces
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(localConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Add .subtask to .gitignore if not already present
	gitignoreAdded := false
	if err := ensureGitignore(cwd); err != nil {
		printWarning(fmt.Sprintf("failed to update .gitignore: %v", err))
	} else {
		gitignoreAdded = true
	}

	// Summary
	fmt.Println()
	fmt.Println(successStyle.Render("  ✓ Setup complete"))
	fmt.Println()
	fmt.Printf("    %s %s\n", subtleStyle.Render("Harness:"), harness)
	if model != "" {
		fmt.Printf("    %s %s\n", subtleStyle.Render("Model:"), model)
	}
	if reasoning != "" {
		fmt.Printf("    %s %s\n", subtleStyle.Render("Reasoning:"), reasoning)
	}
	fmt.Printf("    %s %d\n", subtleStyle.Render("Max workspaces:"), numWorkspaces)
	fmt.Printf("    %s %s\n", subtleStyle.Render("Config:"), localConfigPath)
	if gitignoreAdded {
		fmt.Printf("    %s added to .gitignore\n", subtleStyle.Render("/.subtask/"))
	}
	fmt.Println()

	return nil
}

// isCommandAvailable checks if a command is likely runnable on this machine.
func isCommandAvailable(name string) bool {
	return harness.CanResolveCLI(name)
}

// ensureGitignore adds /.subtask/ to .gitignore if not already ignored.
func ensureGitignore(repoRoot string) error {
	// Use git check-ignore to see if already ignored (handles all gitignore semantics)
	subtaskDir := filepath.Join(repoRoot, ".subtask")
	if err := git.RunQuiet(repoRoot, "check-ignore", "-q", subtaskDir); err == nil {
		return nil // Already ignored
	}

	// Append to .gitignore
	gitignorePath := filepath.Join(repoRoot, ".gitignore")
	pattern := "/.subtask/"

	// Read existing content to check if we need a leading newline
	content, _ := os.ReadFile(gitignorePath)

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add newline before if file exists and doesn't end with newline
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	if _, err := f.WriteString(pattern + "\n"); err != nil {
		return err
	}

	return nil
}
