package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kgruel/subtask/pkg/workspace"
)

// PresetsCmd implements 'subtask presets'.
type PresetsCmd struct {
	JSON bool `help:"Machine-readable JSON output" short:"j"`
}

type presetJSONItem struct {
	Name      string `json:"name"`
	Adapter   string `json:"adapter,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

func (c *PresetsCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	if c.JSON {
		items := []presetJSONItem{}
		for _, name := range sortedKeys(cfg.Presets) {
			p := cfg.Presets[name]
			items = append(items, presetJSONItem{
				Name:      name,
				Adapter:   p.Adapter,
				Model:     p.Model,
				Reasoning: p.Reasoning,
				Provider:  p.Provider,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

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
