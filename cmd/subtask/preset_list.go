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
		fmt.Println("Add presets to .subtask/config.json — see docs/presets.md.")
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

func formatPreset(p workspace.Preset) string {
	parts := []string{}
	if p.Adapter != "" {
		parts = append(parts, p.Adapter)
	}
	if p.Provider != "" {
		parts = append(parts, "provider:"+p.Provider)
	}
	if p.Model != "" {
		parts = append(parts, p.Model)
	}
	if p.Reasoning != "" {
		parts = append(parts, "reasoning:"+p.Reasoning)
	}
	return strings.Join(parts, " / ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
