package config

import (
	"embed"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

//go:embed presets/*.yaml
var presetFS embed.FS

// Preset returns the Config fragment for the named built-in preset.
// The second return value reports whether the name is known.
func Preset(name string) (*Config, bool) {
	data, err := presetFS.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, false
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		// Preset files are embedded at compile time; a parse error is a programming mistake.
		panic("built-in preset " + name + " failed to parse: " + err.Error())
	}
	return &cfg, true
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
