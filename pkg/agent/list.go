package agent

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// AgentSummary is a lightweight description of an agent for listing purposes.
type AgentSummary struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Source       string `json:"source"`
	PresetLabel  string `json:"preset"`
	PromptSource string `json:"prompt"`
}

// List reads .subtask/agents/*.yaml and returns a summary of each agent,
// sorted alphabetically by name. Returns an empty slice (not an error) when
// the agents directory is absent or empty.
func List() ([]AgentSummary, error) {
	dir := AgentsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentSummary{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	summaries := []AgentSummary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		a, err := LoadByName(name)
		if err != nil {
			return nil, fmt.Errorf("load agent %q: %w", name, err)
		}

		presetLabel := a.PresetName
		if a.PresetInline != nil {
			p := a.PresetInline
			parts := []string{p.Adapter, p.Model}
			if p.Reasoning != "" {
				parts = append(parts, p.Reasoning)
			}
			presetLabel = "inline: " + strings.Join(parts, "/")
		}

		promptSrc := "text"
		if a.Prompt.File != "" {
			promptSrc = "file:" + a.Prompt.File
		}

		summaries = append(summaries, AgentSummary{
			Name:         name,
			Description:  a.Description,
			Source:       "project",
			PresetLabel:  presetLabel,
			PromptSource: promptSrc,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}
