package config

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

//go:embed presets/*.yaml
var presetFS embed.FS

// Preset returns the Config fragment for the named built-in preset.
// The second return value reports whether the name is known.
// An error is returned when the file exists but contains invalid YAML.
func Preset(name string) (*Config, bool, error) {
	data, err := presetFS.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, false, nil
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, true, fmt.Errorf("built-in preset %q has invalid YAML: %w", name, err)
	}
	return &cfg, true, nil
}

// PresetNames returns all available built-in preset names in sorted order,
// derived from the filenames in the embedded presets directory.
func PresetNames() []string {
	entries, _ := presetFS.ReadDir("presets")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	sort.Strings(names)
	return names
}
