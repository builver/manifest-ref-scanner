package scanner

import (
	"os/exec"
	"testing"

	"github.com/builver/manifest-ref-scanner/internal/config"
)

func scan(t *testing.T) *Result {
	t.Helper()
	result, err := Scan("testdata/basic", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return result
}

func requireKustomize(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize binary not found in PATH")
	}
}

// TestScan_ExpectedArtifacts verifies that the scanner produces all five expected
// OCI artifact URLs from the testdata fixtures.
func TestScan_ExpectedArtifacts(t *testing.T) {
	result := scan(t)

	byRaw := make(map[string]bool, len(result.Artifacts))
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	want := []string{
		"ghcr.io/example/flux-manifests:v2.0.0", // FluxInstance.distribution.artifact
		"ghcr.io/example/my-gitops:latest",       // Kustomization → synthesized OCIRepository
		"ghcr.io/example/charts/my-app:v1.2.3",  // HelmRelease → tagged OCIRepository
		"ghcr.io/example/charts/versioned",       // HelmRelease → semver OCIRepository (range in Ref["semver"])
		"ghcr.io/example/inline-chart:v3.0.0",   // inline OCIRepository from ResourceSet
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
			t.Errorf("unexpected unresolved artifact: raw=%q warnings=%v", art.Reference, art.Warnings)
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
		if art.Reference != "ghcr.io/example/my-gitops:latest" {
			continue
		}
		for _, src := range art.Sources {
			for _, step := range src.Chain {
				if step.Kind == "OCIRepository" && step.Name == "flux-system" && step.Synthesized {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected ghcr.io/example/my-gitops:latest to have a synthesized OCIRepository/flux-system step in its chain")
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
		if art.Reference != "ghcr.io/example/inline-chart:v3.0.0" {
			continue
		}
		found = true
		inlineStep := false
		for _, src := range art.Sources {
			for _, step := range src.Chain {
				if step.Kind == "OCIRepository" && step.Name == "inline-chart" && step.Inline {
					inlineStep = true
				}
			}
		}
		if !inlineStep {
			t.Error("inline-chart artifact: expected a resolution step with inline=true for OCIRepository/inline-chart")
		}
	}
	if !found {
		t.Error("expected artifact ghcr.io/example/inline-chart:v3.0.0 (only reachable via ResourceSet expansion)")
	}
}

// TestScan_SemverRef verifies that a semver constraint (spec.ref.semver) is stored in
// Ref["semver"] and NOT embedded in the combined reference URL.
func TestScan_SemverRef(t *testing.T) {
	result := scan(t)

	found := false
	for _, art := range result.Artifacts {
		if art.Reference != "ghcr.io/example/charts/versioned" {
			continue
		}
		found = true
		if art.Tag != "" {
			t.Errorf("semver artifact: tag = %q, want empty (range lives in Ref[semver])", art.Tag)
		}
		if art.Ref["semver"] != ">=1.0.0" {
			t.Errorf("semver artifact: Ref[semver] = %q, want >=1.0.0", art.Ref["semver"])
		}
	}
	if !found {
		t.Error("expected artifact ghcr.io/example/charts/versioned not found")
	}
}

// TestScan_NonexistentDir verifies that scanning a missing directory returns an error.
func TestScan_NonexistentDir(t *testing.T) {
	_, err := Scan("testdata/nonexistent-abc123", config.DefaultConfig(), DefaultOptions())
	if err == nil {
		t.Error("expected error for nonexistent directory, got nil")
	}
}

// TestScan_KustomizeOverlay verifies that kustomize build is invoked for a directory
// containing a Kustomize config and that the rendered (patched) tag is used, not the
// tag from the raw source file. The kustomize-simple fixture also contains a helmCharts
// entry; --enable-helm is passed automatically, so the chart's container image must
// also appear in the output.
func TestScan_KustomizeOverlay(t *testing.T) {
	requireKustomize(t)

	result, err := Scan("testdata/kustomize-simple", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	// Patched OCI artifact from the kustomize patch.
	if !byRaw["ghcr.io/example/kustomize-built:v9.9.9-patched"] {
		t.Error("expected rendered artifact ghcr.io/example/kustomize-built:v9.9.9-patched not found")
	}
	if byRaw["ghcr.io/example/kustomize-built:v1.0.0-base"] {
		t.Error("base tag v1.0.0-base should not appear — rendered output should be used")
	}

	// Container image from the helm chart rendered via --enable-helm.
	if !byRaw["xpkg.crossplane.io/crossplane/crossplane:v2.3.2"] {
		t.Error("expected helm chart image xpkg.crossplane.io/crossplane/crossplane:v2.3.2 not found")
	}
}

// TestScan_KustomizeOverlay_HelmChart is a focused test verifying that a helm chart
// inside a kustomize overlay (helmCharts: section) is rendered and its container image
// is extracted. This requires both kustomize and helm to be present.
func TestScan_KustomizeOverlay_HelmChart(t *testing.T) {
	requireKustomize(t)
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not found in PATH")
	}

	result, err := Scan("testdata/kustomize-simple", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	for _, art := range result.Artifacts {
		if art.Reference == "xpkg.crossplane.io/crossplane/crossplane:v2.3.2" {
			found = true
			if art.FieldType != "containerImage" {
				t.Errorf("expected fieldType=containerImage, got %q", art.FieldType)
			}
			break
		}
	}
	if !found {
		t.Error("helm chart container image xpkg.crossplane.io/crossplane/crossplane:v2.3.2 not found in artifacts")
	}
}

// TestScan_KustomizeNoDoubleCount verifies that the rendered artifact appears exactly once,
// i.e. individual files in the overlay directory are not also processed.
func TestScan_KustomizeNoDoubleCount(t *testing.T) {
	requireKustomize(t)

	result, err := Scan("testdata/kustomize-simple", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	count := 0
	for _, art := range result.Artifacts {
		if art.Reference == "ghcr.io/example/kustomize-built:v9.9.9-patched" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of the kustomize-built artifact, got %d", count)
	}
}

// TestScan_KustomizeOverlaysField verifies that the KustomizeOverlays field on the
// artifact is set to the path of the overlay directory that produced it.
func TestScan_KustomizeOverlaysField(t *testing.T) {
	requireKustomize(t)

	result, err := Scan("testdata/kustomize-simple", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, art := range result.Artifacts {
		if art.Reference != "ghcr.io/example/kustomize-built:v9.9.9-patched" {
			continue
		}
		if len(art.KustomizeOverlays) == 0 {
			t.Error("expected KustomizeOverlays to be non-empty for kustomize-rendered artifact")
			return
		}
		got := art.KustomizeOverlays[0]
		if got != "." {
			t.Errorf("KustomizeOverlays[0] = %q, want \".\" (overlay is the scan root)", got)
		}
		return
	}
	t.Error("artifact ghcr.io/example/kustomize-built:v9.9.9-patched not found")
}

// TestScan_DisableKustomize verifies that with DisableKustomize=true the raw files in
// the overlay directory are processed directly, yielding the unpatched tag.
func TestScan_DisableKustomize(t *testing.T) {
	opts := DefaultOptions()
	opts.DisableKustomize = true

	result, err := Scan("testdata/kustomize-simple", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	if !byRaw["ghcr.io/example/kustomize-built:v1.0.0-base"] {
		t.Error("with DisableKustomize=true expected raw artifact ghcr.io/example/kustomize-built:v1.0.0-base")
	}
	if byRaw["ghcr.io/example/kustomize-built:v9.9.9-patched"] {
		t.Error("with DisableKustomize=true the patched tag v9.9.9-patched should not appear")
	}
}

// TestScan_ComplexOverlay_Default verifies the default behaviour on a base+overlays
// structure: staging and production (leaf overlays) are rendered; base is skipped
// because it is referenced as a resource by the other two. Top-level plain YAML files
// outside any overlay directory are always included.
func TestScan_ComplexOverlay_Default(t *testing.T) {
	requireKustomize(t)

	result, err := Scan("testdata/kustomize-overlay", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]int)
	for _, art := range result.Artifacts {
		byRaw[art.Reference]++
	}

	// Leaf overlays must be rendered.
	if byRaw["some.example/oci-repository:staging"] == 0 {
		t.Error("missing staging overlay artifact some.example/oci-repository:staging")
	}
	if byRaw["some.example/oci-repository:production"] == 0 {
		t.Error("missing production overlay artifact some.example/oci-repository:production")
	}

	// Top-level plain file outside any overlay dir must be included.
	if byRaw["some.example/some-other-oci:latest"] == 0 {
		t.Error("missing top-level artifact some.example/some-other-oci:latest from generic-oci.yaml")
	}

	// Base overlay must NOT be rendered as a standalone unit.
	if byRaw["some.example/oci-repository:base"] > 0 {
		t.Error("base overlay should not be rendered independently (it is a dependency of staging/production)")
	}

	// No artifact should appear more than once.
	for raw, count := range byRaw {
		if count > 1 {
			t.Errorf("artifact %q appears %d times, expected exactly 1", raw, count)
		}
	}
}

// TestScan_ComplexOverlay_FilterStaging verifies that KustomizeOverlayFilter limits
// rendering to the matching overlay only. Other overlay directories are skipped
// entirely. Top-level plain YAML files outside any overlay directory are still included.
func TestScan_ComplexOverlay_FilterStaging(t *testing.T) {
	requireKustomize(t)

	opts := DefaultOptions()
	opts.KustomizeOverlayFilter = []string{"staging"}

	result, err := Scan("testdata/kustomize-overlay", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	// Only the staging overlay should be rendered.
	if !byRaw["some.example/oci-repository:staging"] {
		t.Error("missing staging artifact")
	}

	// Top-level file must still be included.
	if !byRaw["some.example/some-other-oci:latest"] {
		t.Error("missing top-level artifact some.example/some-other-oci:latest")
	}

	// Production and base must be absent.
	if byRaw["some.example/oci-repository:production"] {
		t.Error("production overlay should not be rendered when filter=staging")
	}
	if byRaw["some.example/oci-repository:base"] {
		t.Error("base overlay should not appear")
	}
}

// TestScan_ComplexOverlay_NoKustomize verifies that --no-kustomize processes all files
// as plain YAML without any kustomize rendering. The raw base file is read directly
// (unpatched tag), and no rendered overlay variants appear.
func TestScan_ComplexOverlay_NoKustomize(t *testing.T) {
	opts := DefaultOptions()
	opts.DisableKustomize = true

	result, err := Scan("testdata/kustomize-overlay", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	// Raw base file is processed directly → unpatched tag.
	if !byRaw["some.example/oci-repository:base"] {
		t.Error("with --no-kustomize expected raw base artifact some.example/oci-repository:base")
	}

	// Top-level plain file still included.
	if !byRaw["some.example/some-other-oci:latest"] {
		t.Error("with --no-kustomize expected some.example/some-other-oci:latest from generic-oci.yaml")
	}

	// No rendered overlay variants.
	if byRaw["some.example/oci-repository:staging"] {
		t.Error("with --no-kustomize rendered staging artifact should not appear")
	}
	if byRaw["some.example/oci-repository:production"] {
		t.Error("with --no-kustomize rendered production artifact should not appear")
	}
}

// TestScan_HelmChart verifies that a standalone Helm chart directory is rendered
// via `helm template` and its container images are extracted as artifacts.
func TestScan_HelmChart(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not found in PATH")
	}

	result, err := Scan("testdata/helm-standalone", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/helm-standalone:v1.2.3" {
			found = true
			if art.FieldType != "containerImage" {
				t.Errorf("expected fieldType=containerImage, got %q", art.FieldType)
			}
			break
		}
	}
	if !found {
		t.Error("expected helm chart image registry.example.com/helm-standalone:v1.2.3 not found in artifacts")
	}
}

// TestScan_DisableHelm verifies that with DisableHelm=true chart directories are
// skipped silently and their images do not appear in the output.
func TestScan_DisableHelm(t *testing.T) {
	opts := DefaultOptions()
	opts.DisableHelm = true

	result, err := Scan("testdata/helm-standalone", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/helm-standalone:v1.2.3" {
			t.Error("with DisableHelm=true helm chart image should not appear in artifacts")
		}
	}
}
