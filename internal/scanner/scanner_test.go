package scanner

import (
	"testing"

	"github.com/patri/manifest-ref-scanner/internal/config"
)

func scan(t *testing.T) *Result {
	t.Helper()
	result, err := Scan("testdata", config.DefaultConfig())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return result
}

// TestScan_ExpectedArtifacts verifies that the scanner produces all five expected
// OCI artifact URLs from the testdata fixtures.
func TestScan_ExpectedArtifacts(t *testing.T) {
	result := scan(t)

	byRaw := make(map[string]bool, len(result.Artifacts))
	for _, art := range result.Artifacts {
		byRaw[art.Raw] = true
	}

	want := []string{
		"oci://ghcr.io/example/flux-manifests:v2.0.0",   // FluxInstance.distribution.artifact
		"oci://ghcr.io/example/my-gitops:latest",        // Kustomization → synthesized OCIRepository
		"oci://ghcr.io/example/charts/my-app:v1.2.3",   // HelmRelease → tagged OCIRepository
		"oci://ghcr.io/example/charts/versioned:>=1.0.0", // HelmRelease → semver OCIRepository
		"oci://ghcr.io/example/inline-chart:v3.0.0",    // inline OCIRepository from ResourceSet
	}
	for _, raw := range want {
		if !byRaw[raw] {
			t.Errorf("missing expected artifact: %s", raw)
		}
	}
}

// TestScan_NoUnresolvedArtifacts verifies that all sourceRef and chartRef pointers
// in the testdata resolve to registered resources.
func TestScan_NoUnresolvedArtifacts(t *testing.T) {
	result := scan(t)
	for _, art := range result.Artifacts {
		if art.FieldType == "unresolved" {
			t.Errorf("unexpected unresolved artifact: raw=%q warnings=%v", art.Raw, art.Warnings)
		}
	}
}

// TestScan_FluxInstanceSynthesis verifies that the synthesized OCIRepository/flux-system
// (created from FluxInstance.spec.sync — it never appears in any YAML file) appears as a
// synthesized step in the resolution chain of the my-gitops artifact.
func TestScan_FluxInstanceSynthesis(t *testing.T) {
	result := scan(t)

	found := false
	for _, art := range result.Artifacts {
		if art.Raw != "oci://ghcr.io/example/my-gitops:latest" {
			continue
		}
		for _, step := range art.Resolution {
			if step.Kind == "OCIRepository" && step.Name == "flux-system" && step.Synthesized {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected oci://.../my-gitops:latest to have a synthesized OCIRepository/flux-system step in its chain")
	}
}

// TestScan_ResourceSetExpansion verifies that an OCIRepository embedded inside a
// ResourceSet's spec.resources is extracted and its URL appears in the output.
// Because the inline-chart OCIRepository is only reachable through ResourceSet
// expansion, its presence proves the expander ran correctly.
func TestScan_ResourceSetExpansion(t *testing.T) {
	result := scan(t)

	found := false
	for _, art := range result.Artifacts {
		if art.Raw != "oci://ghcr.io/example/inline-chart:v3.0.0" {
			continue
		}
		found = true
		inlineStep := false
		for _, step := range art.Resolution {
			if step.Kind == "OCIRepository" && step.Name == "inline-chart" && step.Inline {
				inlineStep = true
			}
		}
		if !inlineStep {
			t.Error("inline-chart artifact: expected a resolution step with inline=true for OCIRepository/inline-chart")
		}
	}
	if !found {
		t.Error("expected artifact oci://.../inline-chart:v3.0.0 (only reachable via ResourceSet expansion)")
	}
}

// TestScan_SemverRef verifies that a semver constraint (spec.ref.semver) is treated
// as a regular tag: appended to the URL and stored in artifact.tag.
func TestScan_SemverRef(t *testing.T) {
	result := scan(t)

	found := false
	for _, art := range result.Artifacts {
		if art.Raw != "oci://ghcr.io/example/charts/versioned:>=1.0.0" {
			continue
		}
		found = true
		if art.Tag != ">=1.0.0" {
			t.Errorf("semver artifact: tag = %q, want >=1.0.0", art.Tag)
		}
	}
	if !found {
		t.Error("expected artifact oci://ghcr.io/example/charts/versioned:>=1.0.0 not found")
	}
}

// TestScan_NonexistentDir verifies that scanning a missing directory returns an error.
func TestScan_NonexistentDir(t *testing.T) {
	_, err := Scan("testdata/nonexistent-abc123", config.DefaultConfig())
	if err == nil {
		t.Error("expected error for nonexistent directory, got nil")
	}
}
