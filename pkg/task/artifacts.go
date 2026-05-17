package task

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// ArtifactInfo describes a file artifact produced during task execution.
type ArtifactInfo struct {
	Name    string
	Path    string // relative to task folder
	Kind    string // producing step ID (e.g. "review")
	Size    int64  // bytes; 0 if Missing
	Missing bool   // true when file no longer exists on disk
}

type artifactProducedData struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type rawHistoryEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Artifacts returns the artifacts produced during taskName's execution.
// Resolution: reads history.jsonl for artifact.produced events; last-write-wins
// per Path (a re-emitted path replaces the earlier entry). Results are sorted
// by first-emission order. Each entry is stat'd; Missing is set when absent.
func Artifacts(taskName string) ([]ArtifactInfo, error) {
	type entry struct {
		data artifactProducedData
	}

	seen := make(map[string]*entry) // keyed by Path
	var order []string              // first-seen emission order

	histPath := HistoryPath(taskName)
	f, err := os.Open(histPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		// history.jsonl lines can exceed 64 KiB (large reply events); raise the limit
		// so a big message event preceding artifact.produced doesn't silently stop the scan.
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var ev rawHistoryEvent
			if err := json.Unmarshal(line, &ev); err != nil {
				continue
			}
			if ev.Type != "artifact.produced" {
				continue
			}
			var d artifactProducedData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				continue
			}
			if d.Path == "" {
				continue
			}
			if _, ok := seen[d.Path]; !ok {
				order = append(order, d.Path)
			}
			seen[d.Path] = &entry{data: d}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}

	taskDir := Dir(taskName)

	// Prepend well-known task files. TASK.md is always included (Missing=true if absent).
	// PLAN.md is included only when it exists on disk.
	seenPaths := make(map[string]struct{})
	var result []ArtifactInfo
	for _, name := range []string{"TASK.md", "PLAN.md"} {
		abs := filepath.Join(taskDir, name)
		fi, statErr := os.Stat(abs)
		if name == "PLAN.md" && statErr != nil {
			continue
		}
		info := ArtifactInfo{Name: name, Path: name}
		if statErr == nil {
			info.Size = fi.Size()
		} else {
			info.Missing = true
		}
		result = append(result, info)
		seenPaths[name] = struct{}{}
	}

	// Append event-based artifacts not already covered by the well-known set.
	for _, p := range order {
		if _, dup := seenPaths[p]; dup {
			continue
		}
		e := seen[p]
		info := ArtifactInfo{
			Name: e.data.Name,
			Path: e.data.Path,
			Kind: e.data.Kind,
		}
		abs := filepath.Join(taskDir, filepath.FromSlash(p))
		if fi, err := os.Stat(abs); err == nil {
			info.Size = fi.Size()
		} else {
			info.Missing = true
		}
		result = append(result, info)
	}
	return result, nil
}
