package harness

import (
	"embed"
	"errors"
	"fmt"
	"os"
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
