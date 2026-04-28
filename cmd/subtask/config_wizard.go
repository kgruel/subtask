package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/workspace"
)

type configWizardParams struct {
	WritePath string
	RepoRoot  string // optional; used only for display/help text
	Existing  *workspace.Config
	NoPrompt  bool
	// Flag overrides (take precedence over defaults and existing config).
	Adapter       string
	Provider      string
	Model         string
	Reasoning     string
	MaxWorkspaces int
}

type configFlags struct {
	Adapter       string
	Provider      string
	Model         string
	Reasoning     string
	MaxWorkspaces int
}

type configValues struct {
	Adapter       string
	Provider      string
	Model         string
	Reasoning     string
	MaxWorkspaces int
}

// resolveConfigValues merges defaults + existing config + CLI flags into a resolved set of values.
// It is a pure function (no IO) and does not check adapter availability on the machine.
func resolveConfigValues(existing *workspace.Config, flags configFlags) configValues {
	values := configValues{
		Adapter:       "codex",
		MaxWorkspaces: workspace.DefaultMaxWorkspaces,
	}

	if existing != nil {
		if strings.TrimSpace(existing.Adapter) != "" {
			values.Adapter = strings.TrimSpace(existing.Adapter)
		}
		if existing.MaxWorkspaces > 0 {
			values.MaxWorkspaces = existing.MaxWorkspaces
		}
		if strings.TrimSpace(existing.Provider) != "" {
			values.Provider = strings.TrimSpace(existing.Provider)
		}
		if strings.TrimSpace(existing.Model) != "" {
			values.Model = strings.TrimSpace(existing.Model)
		}
		if strings.TrimSpace(existing.Reasoning) != "" {
			values.Reasoning = strings.TrimSpace(existing.Reasoning)
		}
	}

	// Adapter override resets dependent values to adapter-appropriate defaults (but still allows
	// explicit flags to override after).
	if strings.TrimSpace(flags.Adapter) != "" {
		values.Adapter = strings.TrimSpace(flags.Adapter)
		values.Provider = ""
		values.Model = ""
		values.Reasoning = ""
	}
	if strings.TrimSpace(flags.Provider) != "" {
		values.Provider = strings.TrimSpace(flags.Provider)
	}
	if strings.TrimSpace(flags.Model) != "" {
		values.Model = strings.TrimSpace(flags.Model)
	}
	if strings.TrimSpace(flags.Reasoning) != "" {
		values.Reasoning = strings.TrimSpace(flags.Reasoning)
	}
	if flags.MaxWorkspaces > 0 {
		values.MaxWorkspaces = flags.MaxWorkspaces
	}

	// Adapter-specific defaults (only when unset).
	switch strings.TrimSpace(values.Adapter) {
	case "", "codex":
		values.Adapter = "codex"
		if strings.TrimSpace(values.Model) == "" {
			values.Model = "gpt-5.2"
		}
		if strings.TrimSpace(values.Reasoning) == "" {
			values.Reasoning = "high"
		}
	case "claude":
		if strings.TrimSpace(values.Model) == "" {
			values.Model = "opus"
		}
		// If reasoning came from defaults/existing and the user didn't explicitly set it as a flag,
		// drop it for non-codex adapters (keeps config files clean and matches prior behavior).
		if strings.TrimSpace(flags.Reasoning) == "" {
			values.Reasoning = ""
		}
	case "opencode":
		if strings.TrimSpace(flags.Reasoning) == "" {
			values.Reasoning = ""
		}
	}

	return values
}

// validateConfigValues validates resolved values without performing any IO.
func validateConfigValues(values configValues) error {
	adapterName := strings.TrimSpace(values.Adapter)
	userDir := harness.UserAdaptersDir()
	if !harness.AdapterExists(userDir, adapterName) {
		available := harness.ListAdapterNames(userDir)
		return fmt.Errorf("unknown adapter %q\n\nAvailable: %s", adapterName, strings.Join(available, ", "))
	}

	if values.MaxWorkspaces < 0 {
		return fmt.Errorf("max workspaces must be >= 0, got %d", values.MaxWorkspaces)
	}

	return workspace.ValidateReasoningFlag(adapterName, strings.TrimSpace(values.Reasoning))
}

// buildConfig creates a workspace.Config from resolved values.
func buildConfig(values configValues) *workspace.Config {
	cfg := &workspace.Config{
		Adapter:       strings.TrimSpace(values.Adapter),
		Provider:      strings.TrimSpace(values.Provider),
		Model:         strings.TrimSpace(values.Model),
		Reasoning:     strings.TrimSpace(values.Reasoning),
		MaxWorkspaces: values.MaxWorkspaces,
	}
	if cfg.MaxWorkspaces <= 0 {
		cfg.MaxWorkspaces = workspace.DefaultMaxWorkspaces
	}

	return cfg
}

func validateAdapterAvailable(adapterName string) error {
	cliName := harness.CLINameForAdapter(harness.UserAdaptersDir(), adapterName)
	if harness.CanResolveCLI(cliName) {
		return nil
	}
	// Provide install hints for well-known adapters.
	switch adapterName {
	case "codex":
		return fmt.Errorf("codex CLI not found\n\nInstall it from: https://github.com/openai/codex")
	case "claude":
		return fmt.Errorf("claude CLI not found\n\nInstall it from: https://claude.com/claude-code")
	case "opencode":
		return fmt.Errorf("opencode CLI not found\n\nInstall it from: https://github.com/anomalyco/opencode")
	case "gemini":
		return fmt.Errorf("gemini CLI not found\n\nInstall it from: https://github.com/google-gemini/gemini-cli")
	default:
		return fmt.Errorf("%s CLI %q not found\n\nEnsure %q is installed and on your PATH.", adapterName, cliName, cliName)
	}
}

func runConfigWizard(p configWizardParams) (*workspace.Config, bool, error) {
	if strings.TrimSpace(p.WritePath) == "" {
		return nil, false, fmt.Errorf("config write path is required")
	}

	// Discover all adapters whose CLIs are available on this machine.
	userDir := harness.UserAdaptersDir()
	allAdapters := harness.ListAdapterNames(userDir)
	var availableAdapters []string
	for _, name := range allAdapters {
		cliName := harness.CLINameForAdapter(userDir, name)
		if isCommandAvailable(cliName) {
			availableAdapters = append(availableAdapters, name)
		}
	}
	if len(availableAdapters) == 0 {
		return nil, false, fmt.Errorf("no worker adapter available\n\nInstall a supported CLI (codex, claude, opencode, etc.)\nor add a custom adapter YAML to ~/.subtask/adapters/")
	}

	adapterAvailable := func(name string) bool {
		for _, a := range availableAdapters {
			if a == name {
				return true
			}
		}
		return false
	}

	flags := configFlags{
		Adapter:       p.Adapter,
		Provider:      p.Provider,
		Model:         p.Model,
		Reasoning:     p.Reasoning,
		MaxWorkspaces: p.MaxWorkspaces,
	}
	values := resolveConfigValues(p.Existing, flags)

	// If the user didn't explicitly request an adapter and the resolved adapter isn't available,
	// fall back to the first available adapter and reset dependent defaults.
	if strings.TrimSpace(flags.Adapter) == "" && !adapterAvailable(values.Adapter) {
		values = resolveConfigValues(nil, configFlags{
			Adapter:       availableAdapters[0],
			Model:         flags.Model,
			Reasoning:     flags.Reasoning,
			MaxWorkspaces: values.MaxWorkspaces,
		})
	}

	if p.NoPrompt {
		if err := validateConfigValues(values); err != nil {
			return nil, false, err
		}
		if err := validateAdapterAvailable(strings.TrimSpace(values.Adapter)); err != nil {
			return nil, false, err
		}
		cfg := buildConfig(values)
		if err := cfg.SaveTo(p.WritePath); err != nil {
			return nil, false, fmt.Errorf("failed to save config: %w", err)
		}
		_ = harness.CanResolveCLI(cfg.Adapter) // warm discovery
		return cfg, true, nil
	}

	// Use resolved defaults to prefill the wizard.
	h := values.Adapter
	model := values.Model
	reasoning := values.Reasoning
	numWorkspaces := values.MaxWorkspaces

	// Interactive wizard (same flow as prior init).
	firstStep := 0
	if len(availableAdapters) <= 1 {
		firstStep = 1 // skip adapter selection
	}

	step := firstStep
	for {
		// Clear screen and show header + previous answers.
		fmt.Print("\033[H\033[2J")
		fmt.Println()
		fmt.Println("  " + successStyle.Bold(true).Render("Subtask Config"))
		fmt.Println(subtleStyle.Render("  Configure parallel workers"))
		fmt.Println()

		if step > 0 && firstStep == 0 {
			fmt.Printf("  Adapter:   %s\n", h)
		}
		if step > 1 && model != "" {
			fmt.Printf("  Model:     %s\n", model)
		}
		if step > 2 && h == "codex" {
			fmt.Printf("  Reasoning: %s\n", reasoning)
		}
		if step > firstStep {
			fmt.Println()
		}

		var form *huh.Form
		switch step {
		case 0:
			// Well-known display names for built-in adapters.
			displayNames := map[string]string{
				"codex":    "Codex",
				"claude":   "Claude Code",
				"opencode": "OpenCode",
				"gemini":   "Gemini CLI",
			}
			var opts []huh.Option[string]
			for _, name := range availableAdapters {
				label := name
				if d, ok := displayNames[name]; ok {
					label = d
				}
				opts = append(opts, huh.NewOption(label, name))
			}
			form = huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Worker").
					Description("Which CLI runs your tasks behind the scenes").
					Options(opts...).
					Value(&h),
			))

		case 1:
			if h == "codex" {
				opts := []huh.Option[string]{
					huh.NewOption("gpt-5.2 (recommended)", "gpt-5.2"),
					huh.NewOption("gpt-5.2-codex", "gpt-5.2-codex"),
				}
				form = huh.NewForm(huh.NewGroup(
					huh.NewSelect[string]().
						Title("Model").
						Description("Default for workers. Change anytime with: subtask config").
						Options(opts...).
						Value(&model),
				))
			} else if h == "claude" {
				opts := []huh.Option[string]{
					huh.NewOption("Opus (recommended)", "opus"),
					huh.NewOption("Sonnet", "sonnet"),
				}
				form = huh.NewForm(huh.NewGroup(
					huh.NewSelect[string]().
						Title("Model").
						Description("Default for workers. Change anytime with: subtask config").
						Options(opts...).
						Value(&model),
				))
			} else if h == "gemini" {
				form = huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Model (optional)").
						Description("Default for workers. Leave blank to use Gemini's auto router. Change anytime with: subtask config").
						Placeholder("e.g. gemini-2.5-pro").
						Value(&model),
				))
			} else {
				form = huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Model (optional)").
						Description("Default for workers. Leave blank for adapter default. Change anytime with: subtask config").
						Placeholder("provider/model").
						Value(&model),
				))
			}

		case 2:
			if h != "codex" {
				step++
				continue
			}
			form = huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Reasoning").
					Description("Default for workers. Change anytime with: subtask config").
					Options(
						huh.NewOption("Extra High", "xhigh"),
						huh.NewOption("High (recommended)", "high"),
						huh.NewOption("Medium", "medium"),
						huh.NewOption("Low", "low"),
					).
					Value(&reasoning),
			))
		}

		if step > 2 {
			break
		}

		km := huh.NewDefaultKeyMap()
		km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "back"))
		km.Select.Filter = key.NewBinding(key.WithDisabled())
		form = form.WithKeyMap(km).WithTheme(huh.ThemeCharm()).WithShowHelp(true)

		err := form.Run()
		if err == huh.ErrUserAborted {
			if step == firstStep {
				return nil, false, fmt.Errorf("config cancelled")
			}
			step--
			if step == 2 && h != "codex" {
				step--
			}
			continue
		}
		if err != nil {
			return nil, false, err
		}

		// Reset dependent values when adapter changes.
		if step == 0 {
			resolved := resolveConfigValues(nil, configFlags{Adapter: h})
			model = resolved.Model
			reasoning = resolved.Reasoning
		}

		step++
	}

	values = configValues{
		Adapter:       h,
		Provider:      values.Provider,
		Model:         model,
		Reasoning:     reasoning,
		MaxWorkspaces: numWorkspaces,
	}

	// Final validation - ensure selections are valid and adapter is available.
	if err := validateConfigValues(values); err != nil {
		return nil, false, err
	}
	if err := validateAdapterAvailable(strings.TrimSpace(values.Adapter)); err != nil {
		return nil, false, err
	}

	cfg := buildConfig(values)
	if err := cfg.SaveTo(p.WritePath); err != nil {
		return nil, false, fmt.Errorf("failed to save config: %w", err)
	}

	// Warm adapter discovery for better UX on first run.
	_ = harness.CanResolveCLI(cfg.Adapter)

	return cfg, true, nil
}
