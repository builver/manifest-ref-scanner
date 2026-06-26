package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// DefaultConfig returns the built-in field types, synthesizers, and resolvers
// covering standard Kubernetes resources and common Flux CRDs.
func DefaultConfig() *Config {
	return &Config{
		FieldTypes: []FieldType{
			{
				Name: "containerImage",
				Targets: []FieldTarget{
					// Core workloads — containers and initContainers
					{Kind: "Pod", Path: "spec/containers[*]/image"},
					{Kind: "Pod", Path: "spec/initContainers[*]/image"},
					{Kind: "Pod", Path: "spec/ephemeralContainers[*]/image"},
					{Kind: "Deployment", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Deployment", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "DaemonSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "DaemonSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "StatefulSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "StatefulSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "ReplicaSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Job", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Job", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/containers[*]/image"},
					{Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/initContainers[*]/image"},
				},
			},
			{
				Name: "ociArtifact",
				Targets: []FieldTarget{
					// Flux OCIRepository — the canonical OCI source.
					// spec/ref/tag is a real OCI tag; spec/ref/semver is a range selector (not a tag).
					{Group: "source.toolkit.fluxcd.io", Kind: "OCIRepository", Path: "spec/url", TagPaths: []string{"spec/ref/tag"}, SemverPaths: []string{"spec/ref/semver"}},
					// FluxInstance distribution artifact (fully merged ref)
					{Group: "fluxcd.controlplane.io", Kind: "FluxInstance", Path: "spec/distribution/artifact"},
					// HelmRepository (OCI-based)
					{Group: "source.toolkit.fluxcd.io", Kind: "HelmRepository", Path: "spec/url"},
				},
			},
		},

		Synthesizers: []Synthesizer{
			{
				Name:      "fluxInstanceSync",
				FromGroup: "fluxcd.controlplane.io",
				FromKind:  "FluxInstance",
				// FluxInstance.spec.sync causes the Flux Operator to create an OCIRepository
				// named "flux-system" in the same namespace. We synthesize it so resolvers
				// can follow sourceRef.name=flux-system back to the real URL and tag.
				Emits: SynthesizedObject{
					APIVersion: "source.toolkit.fluxcd.io/v1",
					Kind:       "OCIRepository",
					Name:       "flux-system",
					Namespace:  "{{.metadata.namespace}}",
					Spec: map[string]interface{}{
						"url": "{{.spec.sync.url}}",
						"ref": map[string]interface{}{
							"tag": "{{.spec.sync.ref}}",
						},
					},
				},
			},
		},

		Resolvers: []Resolver{
			{
				Name:      "kustomizationSourceRef",
				FromGroup: "kustomize.toolkit.fluxcd.io",
				FromKind:  "Kustomization",
				Path:      "spec/sourceRef",
				Resolves: ResolveTarget{
					Kind:      "{{.kind}}",
					Name:      "{{.name}}",
					Namespace: "{{.namespace}}",
				},
			},
			{
				Name:      "helmReleaseChartRef",
				FromGroup: "helm.toolkit.fluxcd.io",
				FromKind:  "HelmRelease",
				Path:      "spec/chartRef",
				Resolves: ResolveTarget{
					Kind:      "{{.kind}}",
					Name:      "{{.name}}",
					Namespace: "{{.namespace}}",
				},
			},
			{
				Name:      "helmReleaseSourceRef",
				FromGroup: "helm.toolkit.fluxcd.io",
				FromKind:  "HelmRelease",
				Path:      "spec/chart/spec/sourceRef",
				Resolves: ResolveTarget{
					Kind:      "{{.kind}}",
					Name:      "{{.name}}",
					Namespace: "{{.namespace}}",
				},
			},
		},

		InlineExpanders: []InlineExpander{
			{
				FromGroup:          "fluxcd.controlplane.io",
				FromKind:           "ResourceSet",
				ResourcesPath:      "spec/resources",
				InputsPath:         "spec/inputs",
				TemplateDelimLeft:  "<<",
				TemplateDelimRight: ">>",
			},
		},
	}
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
		FieldTypes:           append(append([]FieldType{}, base.FieldTypes...), overlay.FieldTypes...),
		Synthesizers:         append(append([]Synthesizer{}, base.Synthesizers...), overlay.Synthesizers...),
		Resolvers:            append(append([]Resolver{}, base.Resolvers...), overlay.Resolvers...),
		InlineExpanders: append(append([]InlineExpander{}, base.InlineExpanders...), overlay.InlineExpanders...),
	}
}
