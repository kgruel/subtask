package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// ConfigCmd implements 'subtask config'.
type ConfigCmd struct {
	User          bool   `help:"Edit user config (~/.subtask/config.json)"`
	Project       bool   `help:"Edit project config (<git-root>/.subtask/config.json)"`
	NoPrompt      bool   `help:"Non-interactive; use defaults"`
	Adapter       string `help:"Worker adapter (built-in: codex, claude, opencode, pi, gemini; or any custom adapter)" placeholder:"ADAPTER"`
	Provider      string `help:"Provider for the adapter (adapter-dependent)" placeholder:"PROVIDER"`
	Model         string `help:"Default model for workers" placeholder:"MODEL"`
	Reasoning     string `help:"Reasoning level: 'low', 'medium', 'high', 'xhigh' (adapter-dependent)" placeholder:"LEVEL"`
	MaxWorkspaces int    `help:"Max parallel git worktrees per repo (default 20)" placeholder:"N"`
}

func (c *ConfigCmd) Run() error {
	if c.User && c.Project {
		return fmt.Errorf("--user and --project are mutually exclusive")
	}

	scope := "user"
	if c.Project {
		scope = "project"
	} else if !c.User && !c.NoPrompt {
		// Interactive scope prompt.
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Config scope").
				Options(
					huh.NewOption("User (global defaults)", "user"),
					huh.NewOption("Project (this repo only)", "project"),
				).
				Value(&scope),
		))
		if err := form.Run(); err != nil {
			return err
		}
	}

	var path string
	var repoRoot string
	switch scope {
	case "user":
		path = task.ConfigPath()
	case "project":
		var err error
		repoRoot, err = preflightProjectOnly() // requires git; also runs layout migration.
		if err != nil {
			return err
		}
		path = filepath.Join(repoRoot, ".subtask", "config.json")
	default:
		return fmt.Errorf("invalid scope %q", scope)
	}

	// If the user provided enough flags to fully specify the config, skip the wizard.
	noPrompt := c.NoPrompt || (c.Adapter != "" && c.Model != "")

	existing := readConfigFileOrNil(path)
	cfg, wrote, err := runConfigWizard(configWizardParams{
		WritePath:     path,
		RepoRoot:      repoRoot,
		Existing:      existing,
		NoPrompt:      noPrompt,
		Adapter:       c.Adapter,
		Provider:      c.Provider,
		Model:         c.Model,
		Reasoning:     c.Reasoning,
		MaxWorkspaces: c.MaxWorkspaces,
	})
	if err != nil {
		return err
	}
	if !wrote || cfg == nil {
		return nil
	}

	// Best-effort: ignore portable subtask data in git repos.
	if scope == "project" && repoRoot != "" {
		_ = ensureGitignore(repoRoot)
	}

	fmt.Println()
	fmt.Println(successStyle.Render("  ✓ Config saved"))
	printConfigDetails(cfg, scope, path)
	fmt.Println()
	return nil
}

func readConfigFileOrNil(path string) *workspace.Config {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg workspace.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		// Leave validation/reporting to workspace.LoadConfig() for runtime commands.
		return nil
	}
	cfg.MigrateLegacyPublic()
	return &cfg
}

// printConfigDetails prints the config settings in a consistent format.
func printConfigDetails(cfg *workspace.Config, scope, path string) {
	// Title case the scope.
	scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
	fmt.Printf("    %s %s %s\n", subtleStyle.Render("Scope:"), scopeTitle, subtleStyle.Render("("+abbreviatePath(path)+")"))
	fmt.Printf("    %s %s\n", subtleStyle.Render("Adapter:"), cfg.Adapter)
	if strings.TrimSpace(cfg.Provider) != "" {
		fmt.Printf("    %s %s\n", subtleStyle.Render("Provider:"), cfg.Provider)
	}
	if strings.TrimSpace(cfg.Model) != "" {
		fmt.Printf("    %s %s\n", subtleStyle.Render("Model:"), cfg.Model)
	}
	if strings.TrimSpace(cfg.Reasoning) != "" {
		fmt.Printf("    %s %s\n", subtleStyle.Render("Reasoning:"), cfg.Reasoning)
	}
	fmt.Printf("    %s %d\n", subtleStyle.Render("Max workspaces:"), cfg.MaxWorkspaces)
}
