package output

import (
	"io"

	"github.com/patri/manifest-ref-scanner/internal/registry"
	"sigs.k8s.io/yaml"
)

// Formatter renders scan artifacts to a writer.
type Formatter interface {
	Format(w io.Writer, artifacts []*registry.Artifact) error
}

// YAMLFormatter is the default output: a YAML document with an "artifacts" key.
type YAMLFormatter struct{}

type report struct {
	Artifacts []*registry.Artifact `yaml:"artifacts"`
}

func (f *YAMLFormatter) Format(w io.Writer, artifacts []*registry.Artifact) error {
	data, err := yaml.Marshal(report{Artifacts: artifacts})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// WriteYAML is the original package-level helper, kept for compatibility.
func WriteYAML(w io.Writer, artifacts []*registry.Artifact) error {
	return (&YAMLFormatter{}).Format(w, artifacts)
}
