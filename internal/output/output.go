package output

import (
	"io"

	"github.com/patri/manifest-ref-scanner/internal/registry"
	"sigs.k8s.io/yaml"
)

type Report struct {
	Artifacts []*registry.Artifact `yaml:"artifacts"`
}

// WriteYAML marshals the artifact list as a YAML report to w.
func WriteYAML(w io.Writer, artifacts []*registry.Artifact) error {
	data, err := yaml.Marshal(Report{Artifacts: artifacts})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
