package harness

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed adapters/*.yaml
var embeddedAdapters embed.FS

// LoadBuiltinAdapter loads a built-in adapter config by name from the embedded YAML files.
func LoadBuiltinAdapter(name string) (*AdapterConfig, error) {
	path := fmt.Sprintf("adapters/%s.yaml", name)
	data, err := embeddedAdapters.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("built-in adapter %q not found", name)
	}
	return parseAdapterConfig(data)
}

// LoadAdapter loads an adapter config with user override fallback.
// It first checks userDir for <name>.yaml; if not found, falls back to the
// built-in embedded adapter.
func LoadAdapter(userDir, name string) (*AdapterConfig, error) {
	// Try user override first.
	if userDir != "" {
		cfg, err := LoadAdapterConfigFromDir(userDir, name)
		if err == nil {
			return cfg, nil
		}
		// Only fall through on file-not-found; propagate parse errors.
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	// Fall back to built-in.
	return LoadBuiltinAdapter(name)
}

// AdapterExists reports whether an adapter with the given name can be loaded
// from either the user directory or the built-in embedded adapters.
func AdapterExists(userDir, name string) bool {
	_, err := LoadAdapter(userDir, name)
	return err == nil
}

// ListAdapterNames returns the deduplicated, sorted names of all available adapters
// (built-in + user directory). User adapters that override built-in names appear once.
func ListAdapterNames(userDir string) []string {
	seen := make(map[string]struct{})

	// Built-in adapters.
	entries, err := embeddedAdapters.ReadDir("adapters")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			seen[strings.TrimSuffix(e.Name(), ".yaml")] = struct{}{}
		}
	}

	// User adapters.
	if userDir != "" {
		dirEntries, err := os.ReadDir(userDir)
		if err == nil {
			for _, e := range dirEntries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				seen[strings.TrimSuffix(e.Name(), ".yaml")] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// CLINameForAdapter returns the CLI executable name declared in an adapter's config.
// Falls back to the adapter name itself if the adapter YAML cannot be loaded.
func CLINameForAdapter(userDir, adapterName string) string {
	cfg, err := LoadAdapter(userDir, adapterName)
	if err != nil {
		return adapterName
	}
	if cfg.CLI != "" {
		return cfg.CLI
	}
	return adapterName
}

// UserAdaptersDir returns the conventional path for user adapter overrides.
func UserAdaptersDir() string {
	return filepath.Join(globalDir(), "adapters")
}

// globalDir returns the subtask global directory. This duplicates task.GlobalDir()
// to avoid a circular import (harness must not import task for this helper).
func globalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".subtask")
}
