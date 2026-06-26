package scanner

import (
	"testing"

	"github.com/builver/manifest-ref-scanner/internal/config"
)

// loadConfig is a test helper that loads a config file from testdata/example-configs
// and merges it with the default config.
func loadConfig(t *testing.T, name string) *config.Config {
	t.Helper()
	userCfg, err := config.Load("testdata/example-configs/" + name)
	if err != nil {
		t.Fatalf("load config %s: %v", name, err)
	}
	return config.Merge(config.DefaultConfig(), userCfg)
}

// --- Exclude ---

// TestScan_Exclude verifies that directories matching the ExcludeGlobs option are
// skipped entirely. The fixture has two subdirectories; only the one NOT matching
// the glob should contribute artifacts.
func TestScan_Exclude(t *testing.T) {
	opts := DefaultOptions()
	opts.ExcludeGlobs = []string{"skip"}

	result, err := Scan("testdata/exclude-test", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	if !byRaw["registry.example.com/included-repo:v1.0.0-include"] {
		t.Error("expected artifact from include/ dir to be present")
	}
	if byRaw["registry.example.com/excluded-repo:v1.0.0-exclude"] {
		t.Error("artifact from skip/ dir should not appear — directory was excluded")
	}
}

// TestScan_Exclude_PathGlob verifies that a glob matching the relative path
// (not just the base name) works. The fixture has a "deep/skip" subdirectory;
// the glob "deep/skip" must exclude it but leave the top-level "skip" dir alone
// (different relative path).
func TestScan_Exclude_PathGlob(t *testing.T) {
	opts := DefaultOptions()
	opts.ExcludeGlobs = []string{"deep/skip"}

	result, err := Scan("testdata/exclude-test", config.DefaultConfig(), opts)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	// Top-level skip/ is NOT matched by "deep/skip" — its artifact must appear.
	if !byRaw["registry.example.com/excluded-repo:v1.0.0-exclude"] {
		t.Error("top-level skip/ artifact should be present: glob 'deep/skip' should not match it")
	}
	// deep/skip/ IS matched — its artifact must be absent.
	if byRaw["registry.example.com/deep-excluded-repo:v1.0.0-deep-skip"] {
		t.Error("deep/skip/ artifact should not appear — directory matched the path glob")
	}
}

// --- Config: extend existing field type ---

// TestScan_Config_ExtendFieldType verifies that a user config adding a new target
// to the built-in "ociArtifact" field type causes the scanner to extract OCI URLs
// from a custom CRD (ArtifactSource) it would otherwise ignore.
func TestScan_Config_ExtendFieldType(t *testing.T) {
	cfg := loadConfig(t, "extend-oci-artifact.yaml")

	result, err := Scan("testdata/extend-fieldtype", cfg, DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/my-artifacts:v2.0.0" {
			found = true
			if art.FieldType != "ociArtifact" {
				t.Errorf("expected fieldType=ociArtifact, got %q", art.FieldType)
			}
			break
		}
	}
	if !found {
		t.Error("expected registry.example.com/my-artifacts:v2.0.0 from ArtifactSource CRD not found")
	}
}

// TestScan_Config_ExtendFieldType_DefaultConfigMisses verifies that without the
// custom config the ArtifactSource artifact is NOT found, confirming the config
// extension is actually needed and working.
func TestScan_Config_ExtendFieldType_DefaultConfigMisses(t *testing.T) {
	result, err := Scan("testdata/extend-fieldtype", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/my-artifacts:v2.0.0" {
			t.Error("without the custom config ArtifactSource should not be scanned")
		}
	}
}

// --- Config: new field type ---

// TestScan_Config_NewFieldType verifies that a user config introducing a brand-new
// field type ("platformImage") causes the scanner to extract container images from
// a custom PlatformComponent CRD with the correct field type label.
func TestScan_Config_NewFieldType(t *testing.T) {
	cfg := loadConfig(t, "new-fieldtype-platform-image.yaml")

	result, err := Scan("testdata/new-fieldtype", cfg, DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/api-server:v4.0.0" {
			found = true
			if art.FieldType != "platformImage" {
				t.Errorf("expected fieldType=platformImage, got %q", art.FieldType)
			}
			break
		}
	}
	if !found {
		t.Error("expected registry.example.com/api-server:v4.0.0 from PlatformComponent not found")
	}
}

// --- Config: synthesizer ---

// TestScan_Config_Synthesizer verifies that a user-defined synthesizer creates a
// virtual resource from a custom CRD at scan time.  The fixture has a
// GitOpsEnvironment and a Flux Kustomization that points at the OCIRepository the
// synthesizer produces.  The test checks:
//   - the OCI artifact is present in the output
//   - the resolution chain contains the synthesized OCIRepository step
func TestScan_Config_Synthesizer(t *testing.T) {
	cfg := loadConfig(t, "synthesizer-gitops-env.yaml")

	result, err := Scan("testdata/synthesizer", cfg, DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	syntheticInChain := false
	for _, a := range result.Artifacts {
		if a.Reference != "registry.example.com/gitops/staging:v5.0.0" {
			continue
		}
		found = true
		for _, step := range a.Resolution {
			if step.Kind == "OCIRepository" && step.Synthesized {
				syntheticInChain = true
			}
		}
	}

	if !found {
		t.Error("expected registry.example.com/gitops/staging:v5.0.0 from synthesized OCIRepository not found")
	}
	if !syntheticInChain {
		t.Error("expected a synthesized OCIRepository step in the resolution chain")
	}
}

// TestScan_Config_Synthesizer_NoConfig verifies that without the synthesizer config
// the Kustomization's sourceRef is reported as unresolved (the OCIRepository it
// points at does not exist in any file).
func TestScan_Config_Synthesizer_NoConfig(t *testing.T) {
	result, err := Scan("testdata/synthesizer", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	hasUnresolved := false
	for _, art := range result.Artifacts {
		if art.FieldType == "unresolved" {
			hasUnresolved = true
			break
		}
	}
	if !hasUnresolved {
		t.Error("without synthesizer config the Kustomization sourceRef should be unresolved")
	}
}

// --- Config: resolver ---

// TestScan_Config_Resolver verifies that a user-defined resolver follows a custom
// reference chain (AppDeployment → ArtifactBundle) and that the resolution chain
// in the output reflects both hops.
func TestScan_Config_Resolver(t *testing.T) {
	cfg := loadConfig(t, "resolver-app-deployment.yaml")

	result, err := Scan("testdata/resolver", cfg, DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	found := false
	hasDeploymentStep := false
	hasBundleStep := false
	for _, art := range result.Artifacts {
		if art.Reference != "registry.example.com/app-bundle:v6.0.0" {
			continue
		}
		found = true
		for _, step := range art.Resolution {
			if step.Kind == "AppDeployment" && step.Name == "my-app" {
				hasDeploymentStep = true
			}
			if step.Kind == "ArtifactBundle" && step.Name == "my-bundle" {
				hasBundleStep = true
			}
		}
	}

	if !found {
		t.Error("expected registry.example.com/app-bundle:v6.0.0 not found")
	}
	if !hasDeploymentStep {
		t.Error("expected AppDeployment/my-app in resolution chain")
	}
	if !hasBundleStep {
		t.Error("expected ArtifactBundle/my-bundle in resolution chain")
	}
}

// TestScan_Config_Resolver_NoConfig verifies that without the custom config the
// ArtifactBundle is not a known OCI source and the AppDeployment chain is not
// followed, so the artifact does not appear in the output.
func TestScan_Config_Resolver_NoConfig(t *testing.T) {
	result, err := Scan("testdata/resolver", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/app-bundle:v6.0.0" {
			t.Error("without custom resolver config the ArtifactBundle artifact should not appear")
		}
	}
}

// --- Config: inline expander ---

// TestScan_Config_InlineExpander verifies that a user-defined inline expander
// materialises one OCIRepository per environment entry from an ApplicationSet
// and that both resulting OCI artifacts appear in the output.
func TestScan_Config_InlineExpander(t *testing.T) {
	cfg := loadConfig(t, "inline-expander-app-set.yaml")

	result, err := Scan("testdata/inline-expander", cfg, DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byRaw := make(map[string]bool)
	for _, art := range result.Artifacts {
		byRaw[art.Reference] = true
	}

	if !byRaw["registry.example.com/myapp:v7.0.0-staging"] {
		t.Error("missing staging artifact registry.example.com/myapp:v7.0.0-staging")
	}
	if !byRaw["registry.example.com/myapp:v7.0.0-prod"] {
		t.Error("missing production artifact registry.example.com/myapp:v7.0.0-prod")
	}
}

// TestScan_Config_InlineExpander_NoConfig verifies that without the custom expander
// config the ApplicationSet is treated as an opaque resource and no OCIRepository
// artifacts are produced from it.
func TestScan_Config_InlineExpander_NoConfig(t *testing.T) {
	result, err := Scan("testdata/inline-expander", config.DefaultConfig(), DefaultOptions())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, art := range result.Artifacts {
		if art.Reference == "registry.example.com/myapp:v7.0.0-staging" ||
			art.Reference == "registry.example.com/myapp:v7.0.0-prod" {
			t.Errorf("without custom expander config ApplicationSet artifacts should not appear: %s", art.Reference)
		}
	}
}
