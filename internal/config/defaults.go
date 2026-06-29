package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// DefaultConfig returns the merged result of all built-in presets.
// It is equivalent to running the tool with no --no-preset flags.
func DefaultConfig() *Config {
	cfg := &Config{}
	for _, name := range PresetNames() {
		preset, _, err := Preset(name)
		if err != nil || preset == nil {
			continue
		}
		cfg = Merge(cfg, preset)
	}
	return cfg
}

// Load reads a config YAML file and returns its contents.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

// Merge appends overlay's entries after base's entries in every section.
// The base is never modified; a new Config is returned.
func Merge(base, overlay *Config) *Config {
	return &Config{
		FieldTypes:      append(append([]FieldType{}, base.FieldTypes...), overlay.FieldTypes...),
		Synthesizers:    append(append([]Synthesizer{}, base.Synthesizers...), overlay.Synthesizers...),
		Resolvers:       append(append([]Resolver{}, base.Resolvers...), overlay.Resolvers...),
		InlineExpanders: append(append([]InlineExpander{}, base.InlineExpanders...), overlay.InlineExpanders...),
		SuppressedKinds: append(append([]SuppressedKind{}, base.SuppressedKinds...), overlay.SuppressedKinds...),
	}
}
