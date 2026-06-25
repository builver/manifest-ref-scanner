package output

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// FormatConfig is the schema of a custom output format config file.
// Either Template (inline) or TemplateFile (external path) must be set.
// Args provides default values available in the template as .Args.<key>;
// they are overridden by --arg flags passed at the CLI.
type FormatConfig struct {
	Template     string            `yaml:"template"`
	TemplateFile string            `yaml:"templateFile"`
	Args         map[string]string `yaml:"args"`
}

// Load reads a FormatConfig from a YAML file at path.
func Load(path string) (FormatConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FormatConfig{}, fmt.Errorf("read format config: %w", err)
	}
	var cfg FormatConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return FormatConfig{}, fmt.Errorf("parse format config: %w", err)
	}
	return cfg, nil
}
