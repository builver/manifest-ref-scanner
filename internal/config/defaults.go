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
					// v1 (core group) — Pod is the only workload in the core group.
					{Kind: "Pod", Path: "spec/containers[*]/image"},
					{Kind: "Pod", Path: "spec/initContainers[*]/image"},
					{Kind: "Pod", Path: "spec/ephemeralContainers[*]/image"},
					// apps/v1 workloads
					{Group: "apps", Kind: "Deployment", Path: "spec/template/spec/containers[*]/image"},
					{Group: "apps", Kind: "Deployment", Path: "spec/template/spec/initContainers[*]/image"},
					{Group: "apps", Kind: "DaemonSet", Path: "spec/template/spec/containers[*]/image"},
					{Group: "apps", Kind: "DaemonSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Group: "apps", Kind: "StatefulSet", Path: "spec/template/spec/containers[*]/image"},
					{Group: "apps", Kind: "StatefulSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Group: "apps", Kind: "ReplicaSet", Path: "spec/template/spec/containers[*]/image"},
					// batch/v1 workloads
					{Group: "batch", Kind: "Job", Path: "spec/template/spec/containers[*]/image"},
					{Group: "batch", Kind: "Job", Path: "spec/template/spec/initContainers[*]/image"},
					{Group: "batch", Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/containers[*]/image"},
					{Group: "batch", Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/initContainers[*]/image"},
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

		// SuppressedKinds lists well-known Kubernetes resource types that never
		// carry OCI artifact references. They are silently excluded from the
		// unknown_kinds section of the coverage report.
		// Add repo-specific CRDs (e.g. ClusterIssuer) in your --config file.
		SuppressedKinds: []SuppressedKind{
			// core (v1)
			{Kind: "ConfigMap"},
			{Kind: "Endpoints"},
			{Kind: "LimitRange"},
			{Kind: "Namespace"},
			{Kind: "Node"},
			{Kind: "PersistentVolume"},
			{Kind: "PersistentVolumeClaim"},
			{Kind: "ResourceQuota"},
			{Kind: "Secret"},
			{Kind: "Service"},
			{Kind: "ServiceAccount"},
			// apps/v1
			{Group: "apps", Kind: "ControllerRevision"},
			// networking.k8s.io
			{Group: "networking.k8s.io", Kind: "Ingress"},
			{Group: "networking.k8s.io", Kind: "IngressClass"},
			{Group: "networking.k8s.io", Kind: "NetworkPolicy"},
			// rbac.authorization.k8s.io
			{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole"},
			{Group: "rbac.authorization.k8s.io", Kind: "ClusterRoleBinding"},
			{Group: "rbac.authorization.k8s.io", Kind: "Role"},
			{Group: "rbac.authorization.k8s.io", Kind: "RoleBinding"},
			// storage.k8s.io
			{Group: "storage.k8s.io", Kind: "StorageClass"},
			{Group: "storage.k8s.io", Kind: "VolumeAttachment"},
			// policy
			{Group: "policy", Kind: "PodDisruptionBudget"},
			// autoscaling
			{Group: "autoscaling", Kind: "HorizontalPodAutoscaler"},
			// apiextensions.k8s.io
			{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"},
			// admissionregistration.k8s.io
			{Group: "admissionregistration.k8s.io", Kind: "MutatingWebhookConfiguration"},
			{Group: "admissionregistration.k8s.io", Kind: "ValidatingWebhookConfiguration"},
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
		FieldTypes:      append(append([]FieldType{}, base.FieldTypes...), overlay.FieldTypes...),
		Synthesizers:    append(append([]Synthesizer{}, base.Synthesizers...), overlay.Synthesizers...),
		Resolvers:       append(append([]Resolver{}, base.Resolvers...), overlay.Resolvers...),
		InlineExpanders: append(append([]InlineExpander{}, base.InlineExpanders...), overlay.InlineExpanders...),
		SuppressedKinds: append(append([]SuppressedKind{}, base.SuppressedKinds...), overlay.SuppressedKinds...),
	}
}
