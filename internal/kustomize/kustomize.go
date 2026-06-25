package kustomize

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// IsKustomizeDir reports whether dir contains a Kustomize overlay config file
// (kustomization.yaml or kustomization.yml) that is not a Flux Kustomization CRD.
//
// Returns the config file name (relative), ok=true when the dir is a Kustomize overlay,
// or ok=false when no config file is present or the file belongs to another API group
// (e.g. kustomize.toolkit.fluxcd.io).
func IsKustomizeDir(dir string) (kustomizeFile string, ok bool, err error) {
	for _, name := range []string{"kustomization.yaml", "kustomization.yml"} {
		path := filepath.Join(dir, name)
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return "", false, readErr
		}

		var meta struct {
			APIVersion string `yaml:"apiVersion"`
		}
		if unmarshalErr := yaml.Unmarshal(data, &meta); unmarshalErr != nil {
			// Can't parse — treat as non-overlay to be safe
			return "", false, nil
		}

		// Empty apiVersion or kustomize.config.k8s.io/… → Kustomize overlay config.
		// Any other non-empty apiVersion (e.g. kustomize.toolkit.fluxcd.io/v1) → not an overlay.
		if meta.APIVersion != "" && !isKustomizeConfigAPI(meta.APIVersion) {
			return "", false, nil
		}
		return name, true, nil
	}
	return "", false, nil
}

// isKustomizeConfigAPI reports whether apiVersion belongs to the Kustomize config API group.
func isKustomizeConfigAPI(apiVersion string) bool {
	const prefix = "kustomize.config.k8s.io/"
	return len(apiVersion) >= len(prefix) && apiVersion[:len(prefix)] == prefix
}

// ParseResources reads the kustomization config in dir and returns the local
// directory entries from the resources: list. These are the directories that
// this overlay directly depends on, used to identify base/non-leaf overlays.
func ParseResources(dir, kfile string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, kfile))
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Resources []string `yaml:"resources"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	var dirs []string
	for _, res := range cfg.Resources {
		if isRemoteRef(res) {
			continue
		}
		resolved := filepath.Clean(filepath.Join(dir, res))
		if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
			dirs = append(dirs, resolved)
		}
	}
	return dirs, nil
}

// isRemoteRef reports whether a resource entry is a remote URL rather than a
// local path (remote refs are irrelevant for leaf detection).
func isRemoteRef(s string) bool {
	return strings.Contains(s, "://") ||
		strings.HasPrefix(s, "github.com/") ||
		strings.HasPrefix(s, "gitlab.com/")
}

// Build runs `kustomize build dir --load-restrictor=LoadRestrictionsNone` and
// returns the rendered YAML. Stderr is captured and included in the error on failure.
func Build(dir string) ([]byte, error) {
	path, lookErr := exec.LookPath("kustomize")
	if lookErr != nil {
		return nil, fmt.Errorf("kustomize: executable file not found in $PATH — install kustomize to render overlays")
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(path, "build", dir,
		"--load-restrictor=LoadRestrictionsNone",
		"--enable-helm",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w\n%s", dir, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
