package helm

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// IsHelmChart reports whether dir contains a Chart.yaml, making it a Helm chart root.
// Returns the chart name from Chart.yaml (falling back to the directory base name) and ok=true.
func IsHelmChart(dir string) (name string, ok bool, err error) {
	data, readErr := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	if os.IsNotExist(readErr) {
		return "", false, nil
	}
	if readErr != nil {
		return "", false, readErr
	}

	var meta struct {
		Name string `yaml:"name"`
	}
	if unmarshalErr := yaml.Unmarshal(data, &meta); unmarshalErr != nil {
		return filepath.Base(dir), true, nil
	}
	if meta.Name == "" {
		return filepath.Base(dir), true, nil
	}
	return meta.Name, true, nil
}

// Template runs `helm template <name> <dir>` and returns the rendered YAML.
// Stderr is captured and included in the error on failure.
func Template(name, dir string) ([]byte, error) {
	path, lookErr := exec.LookPath("helm")
	if lookErr != nil {
		return nil, fmt.Errorf("helm: executable file not found in $PATH — install helm to render chart templates")
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(path, "template", name, dir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template %s %s: %w\n%s", name, dir, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
