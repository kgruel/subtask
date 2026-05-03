package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgruel/subtask/pkg/workspace"
)

// PresetsCmd implements 'subtask presets'.
type PresetsCmd struct{}

func (c *PresetsCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	if len(cfg.Presets) == 0 {
		fmt.Println("No presets defined.")
		fmt.Println("Add presets to .subtask/config.json — see docs/types-and-presets.md.")
		return nil
	}

	names := sortedKeys(cfg.Presets)
	fmt.Println("Available presets:")
	for _, name := range names {
		p := cfg.Presets[name]
		fmt.Printf("  %-20s %s\n", name, formatPreset(p))
	}
	return nil
}

// TypesCmd implements 'subtask types'.
type TypesCmd struct{}

func (c *TypesCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	if len(cfg.Types) == 0 {
		fmt.Println("No task types defined.")
		fmt.Println("Add types to .subtask/config.json — see docs/types-and-presets.md.")
		return nil
	}

	names := sortedKeys(cfg.Types)
	fmt.Println("Available task types:")
	for _, name := range names {
		t := cfg.Types[name]
		fmt.Printf("  %-15s %s\n", name, t.Description)
		if t.DefaultWorkflow != "" {
			fmt.Printf("    workflow: %s\n", t.DefaultWorkflow)
		}
		if t.DefaultPreset != "" {
			fmt.Printf("    preset:   %s\n", t.DefaultPreset)
		}
	}
	return nil
}

// applyPreset fills resolved fields from p without overwriting fields already set.
func applyPreset(p workspace.Preset, adapter, provider, model, reasoning *string) {
	if *adapter == "" {
		*adapter = p.Adapter
	}
	if *provider == "" {
		*provider = p.Provider
	}
	if *model == "" {
		*model = p.Model
	}
	if *reasoning == "" {
		*reasoning = p.Reasoning
	}
}

func formatPreset(p workspace.Preset) string {
	parts := []string{}
	if p.Adapter != "" {
		parts = append(parts, p.Adapter)
	}
	if p.Model != "" {
		parts = append(parts, p.Model)
	}
	if p.Reasoning != "" {
		parts = append(parts, "reasoning:"+p.Reasoning)
	}
	return strings.Join(parts, " / ")
}

func presetNames(cfg *workspace.Config) string {
	if len(cfg.Presets) == 0 {
		return "(none defined)"
	}
	return strings.Join(sortedKeys(cfg.Presets), ", ")
}

func typeNames(cfg *workspace.Config) string {
	if len(cfg.Types) == 0 {
		return "(none defined)"
	}
	return strings.Join(sortedKeys(cfg.Types), ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
