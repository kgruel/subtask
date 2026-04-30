package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/kgruel/subtask/internal/homedir"
	"github.com/kgruel/subtask/pkg/install"
	"github.com/kgruel/subtask/pkg/task"
)

// InstallCmd implements 'subtask install'.
type InstallCmd struct {
	Guide         bool   `help:"Print setup guidance and exit"`
	NoPrompt      bool   `help:"Non-interactive; use defaults"`
	Scope         string `help:"Skill scope: 'user' or 'project'" placeholder:"SCOPE"`
	Adapter       string `help:"Worker adapter (built-in: codex, claude, opencode, pi, gemini; or any custom adapter)" placeholder:"ADAPTER"`
	Provider      string `help:"Provider for the adapter (adapter-dependent)" placeholder:"PROVIDER"`
	Model         string `help:"Default model for workers" placeholder:"MODEL"`
	Reasoning     string `help:"Reasoning level: 'low', 'medium', 'high', 'xhigh' (adapter-dependent)" placeholder:"LEVEL"`
	MaxWorkspaces int    `help:"Max parallel git worktrees per repo (default 20)" placeholder:"N"`
	PluginDev     bool   `help:"Symlink the local plugin/ directory into Claude Code's plugins folder for development"`
	From          string `help:"Plugin source directory (default: <repo-root>/plugin); used with --plugin-dev" placeholder:"PATH"`
}

func (c *InstallCmd) Run() error {
	if c.Guide {
		printSetupGuide()
		return nil
	}

	if c.PluginDev {
		return c.runPluginDev()
	}
	if c.From != "" {
		return fmt.Errorf("--from requires --plugin-dev")
	}

	homeDir, err := homedir.Dir()
	if err != nil {
		return err
	}

	once, err := install.RunLegacyClaudePluginMigrationOnce(homeDir)
	if err != nil {
		return err
	}
	if once.Ran && once.Migration.SkippedSettingsMalformed {
		printWarning(fmt.Sprintf("Skipped legacy settings cleanup (malformed JSON at %s)", abbreviatePath(once.Migration.SettingsPath)))
	}
	if once.Ran && (once.Migration.RemovedLegacyPluginDir || once.Migration.RemovedLegacySettingsKey) {
		printSuccess("Removed legacy Claude plugin install artifacts")
	}

	// Determine scope - from flag, interactive, or default.
	// Project scope only makes sense inside a git repository.
	inGitRepo := isInGitRepo()
	scope := c.Scope
	if scope != "" && scope != "user" && scope != "project" {
		return fmt.Errorf("--scope must be 'user' or 'project', got %q", scope)
	}
	if scope == "project" && !inGitRepo {
		return fmt.Errorf("--scope=project requires being in a git repository")
	}
	if scope == "" {
		if c.NoPrompt || !inGitRepo {
			scope = "user"
		} else {
			var err error
			scope, err = runScopeWizard()
			if err != nil {
				return err
			}
		}
	}

	// Install skill to appropriate location.
	var skillPath string
	var updated bool
	if scope == "project" {
		repoRoot := task.ProjectRoot()
		skillPath, updated, err = install.InstallToProject(repoRoot)
	} else {
		skillPath, updated, err = install.InstallTo(homeDir)
	}
	if err != nil {
		return err
	}
	if updated {
		printSuccess(fmt.Sprintf("Installed skill to %s", abbreviatePath(skillPath)))
	} else {
		printSuccess(fmt.Sprintf("Skill already up to date at %s", abbreviatePath(skillPath)))
	}

	// If not configured yet, run the config wizard and write ~/.subtask/config.json.
	if _, err := os.Stat(task.ConfigPath()); os.IsNotExist(err) {
		cfg, _, err := runConfigWizard(configWizardParams{
			WritePath:     task.ConfigPath(),
			Existing:      readConfigFileOrNil(task.ConfigPath()),
			NoPrompt:      c.NoPrompt,
			Adapter:       c.Adapter,
			Provider:      c.Provider,
			Model:         c.Model,
			Reasoning:     c.Reasoning,
			MaxWorkspaces: c.MaxWorkspaces,
		})
		if err != nil {
			return err
		}
		if cfg != nil {
			printSuccess("Configured subtask")
			printConfigDetails(cfg, "user", task.ConfigPath())
		}
	} else if !updated {
		// Skill was already up to date and config exists - let user know how to reconfigure.
		fmt.Println()
		fmt.Println("Subtask is already installed. To change configuration:")
		fmt.Println("  subtask config        # edit global defaults")
		fmt.Println("  subtask config --project  # edit project overrides")
	}

	printPluginGuidance(homeDir)

	return nil
}

// runPluginDev handles `subtask install --plugin-dev`. It symlinks a local
// plugin source directory into ~/.claude/plugins/subtask so working-tree edits
// take effect immediately. Default source is <git-root>/plugin; override with
// --from.
func (c *InstallCmd) runPluginDev() error {
	source := c.From
	if source == "" {
		root, err := task.GitRootAbs()
		if err != nil || root == "" {
			return fmt.Errorf("--plugin-dev requires --from <path> when not run inside a git repository")
		}
		source = filepath.Join(root, "plugin")
	}

	res, err := install.InstallPluginDev(source)
	if err != nil {
		return err
	}

	switch res.Action {
	case "created":
		printSuccess(fmt.Sprintf("Linked plugin: %s -> %s", abbreviatePath(res.Path), abbreviatePath(res.SourceDir)))
	case "updated":
		printSuccess(fmt.Sprintf("Updated plugin link: %s -> %s", abbreviatePath(res.Path), abbreviatePath(res.SourceDir)))
	case "noop":
		printSuccess(fmt.Sprintf("Plugin already linked: %s -> %s", abbreviatePath(res.Path), abbreviatePath(res.SourceDir)))
	}

	fmt.Println()
	fmt.Println("Restart Claude Code to pick up plugin changes.")
	return nil
}

// printPluginGuidance shows next-step instructions about the plugin (hooks)
// based on its current install state.
func printPluginGuidance(homeDir string) {
	st, err := install.GetPluginStatusFor(homeDir)
	if err != nil {
		return
	}

	fmt.Println()
	switch {
	case !st.Exists:
		fmt.Println("Hooks (optional):")
		fmt.Println("  Subtask ships hooks that surface unread worker replies and reduce")
		fmt.Println("  the chance of misreading background-task completions. To enable in")
		fmt.Println("  Claude Code:")
		fmt.Println()
		fmt.Println("    /plugin marketplace add github:kgruel/subtask")
		fmt.Println("    /plugin install subtask@subtask")
		fmt.Println()
		fmt.Println("  Or for plugin development: subtask install --plugin-dev")
	case st.IsSymlink && st.HasManifest:
		fmt.Printf("Plugin: linked (dev) at %s -> %s\n", abbreviatePath(st.Path), abbreviatePath(st.SymlinkTarget))
	case st.HasManifest:
		fmt.Printf("Plugin: installed at %s\n", abbreviatePath(st.Path))
	default:
		printWarning(fmt.Sprintf("Plugin path %s exists but is missing .claude-plugin/plugin.json — Claude Code may not load it.", abbreviatePath(st.Path)))
	}
}

func printSetupGuide() {
	type guideData struct {
		InGitRepo          bool
		CodexAvailable     bool
		ClaudeAvailable    bool
		OpencodeAvailable  bool
		PiAvailable        bool
		GeminiAvailable    bool
		AnyAdapterAvailable bool
		MultipleAdapters   bool
	}

	data := guideData{
		InGitRepo:         isInGitRepo(),
		CodexAvailable:    isCommandAvailable("codex"),
		ClaudeAvailable:   isCommandAvailable("claude"),
		OpencodeAvailable: isCommandAvailable("opencode"),
		PiAvailable:       isCommandAvailable("pi"),
		GeminiAvailable:   isCommandAvailable("gemini"),
	}
	count := 0
	if data.CodexAvailable {
		count++
	}
	if data.ClaudeAvailable {
		count++
	}
	if data.OpencodeAvailable {
		count++
	}
	if data.PiAvailable {
		count++
	}
	if data.GeminiAvailable {
		count++
	}
	data.AnyAdapterAvailable = count > 0
	data.MultipleAdapters = count > 1

	const tpl = `# Setup Subtask

**You (Claude Code) are the lead.** Subtask lets you create tasks, spawn subagents, track progress, review their work, and request changes. Each task runs in its own git worktree so they can work in parallel safely. The user doesn't run subtask commands — you do.

## Environment

{{if .InGitRepo}}✓ In a git repository{{else}}⚠ Not in a git repository (you'll need one later to create tasks){{end}}

**Available worker adapters:**
{{if .CodexAvailable}}- ✓ Codex CLI (recommended)
{{else}}- ✗ Codex CLI not found — install from https://github.com/openai/codex
{{end}}{{if .ClaudeAvailable}}- ✓ Claude Code CLI
{{else}}- ✗ Claude Code CLI not found — install from https://claude.ai/download
{{end}}{{if .OpencodeAvailable}}- ✓ OpenCode CLI
{{else}}- ✗ OpenCode CLI not found — install from https://github.com/sst/opencode
{{end}}{{if .PiAvailable}}- ✓ Pi CLI
{{else}}- ✗ Pi CLI not found
{{end}}{{if .GeminiAvailable}}- ✓ Gemini CLI
{{else}}- ✗ Gemini CLI not found — install from https://github.com/google-gemini/gemini-cli
{{end}}
{{if not .AnyAdapterAvailable}}**No adapter available.** Install at least one (Codex recommended) before proceeding.
{{end}}
## Install
{{if .AnyAdapterAvailable}}
{{if .MultipleAdapters}}Ask the user which adapter they'd like to use for workers. Then run:

` + "```bash" + `
subtask install --no-prompt --adapter <name>
` + "```" + `
{{else}}Run the install command:

` + "```bash" + `
subtask install --no-prompt
` + "```" + `
{{end}}
The install:
1. Installs the Subtask skill to ~/.claude/skills/subtask/
2. Writes config to ~/.subtask/config.json (with sensible defaults for model, etc.)

The user can change adapter, model, or other settings later with ` + "`subtask config`" + `.
{{else}}
First install a worker adapter, then run:

` + "```bash" + `
subtask install --no-prompt
` + "```" + `
{{end}}
## Ready
{{if not .InGitRepo}}
**Before creating tasks:** You're not in a git repository. If this looks like a project directory, offer to run ` + "`git init`" + `. Otherwise, ask the user where their project is.
{{end}}After install, load the Subtask skill with ` + "`/subtask`" + ` to get the full workflow instructions.

Then suggest example tasks adapted to the project, like:
- "Fix the login bug with Subtask"
- "Run these 3 features in parallel"
- "Plan and implement the new API endpoint with Subtask"

Once you start your first task, let the user know they can run ` + "`subtask`" + ` in a separate terminal to watch progress in the TUI.`

	t := template.Must(template.New("guide").Parse(tpl))
	if err := t.Execute(os.Stdout, data); err != nil {
		fmt.Fprintf(os.Stderr, "template error: %v\n", err)
	}
}

func isInGitRepo() bool {
	root, err := task.GitRootAbs()
	return err == nil && root != ""
}

func runScopeWizard() (string, error) {
	scope := "user"

	// Clear screen and show header.
	fmt.Print("\033[H\033[2J")
	fmt.Println()
	fmt.Println("  " + successStyle.Bold(true).Render("Install Claude Code Skill"))
	fmt.Println(subtleStyle.Render("  The skill teaches Claude Code the subtask commands and workflow"))
	fmt.Println()

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Where to install the Claude Skill?").
			Options(
				huh.NewOption("Globally (recommended)", "user"),
				huh.NewOption("This project only", "project"),
			).
			Value(&scope),
	))

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	km.Select.Filter = key.NewBinding(key.WithDisabled())
	form = form.WithKeyMap(km).WithTheme(huh.ThemeCharm()).WithShowHelp(true)

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", fmt.Errorf("install cancelled")
		}
		return "", err
	}

	return scope, nil
}
