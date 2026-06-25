package kustomize

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeKustomizationFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsKustomizeDir_KustomizeConfig(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n")
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for kustomize.config.k8s.io/v1beta1")
	}
}

func TestIsKustomizeDir_KustomizeConfigV1(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "apiVersion: kustomize.config.k8s.io/v1\nkind: Kustomization\n")
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for kustomize.config.k8s.io/v1")
	}
}

func TestIsKustomizeDir_NoAPIVersion(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "resources:\n  - deployment.yaml\n")
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true when apiVersion is absent")
	}
}

func TestIsKustomizeDir_FluxCRD(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\n")
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for Flux Kustomization CRD (kustomize.toolkit.fluxcd.io/v1)")
	}
}

func TestIsKustomizeDir_FluxCRDv1beta2(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "apiVersion: kustomize.toolkit.fluxcd.io/v1beta2\nkind: Kustomization\n")
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for Flux Kustomization CRD (v1beta2)")
	}
}

func TestIsKustomizeDir_NoFile(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when no kustomization file is present")
	}
}

func TestIsKustomizeDir_KustomizeYml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kustomization.yml"), []byte("resources: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kfile, ok, err := IsKustomizeDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for kustomization.yml")
	}
	if kfile != "kustomization.yml" {
		t.Errorf("expected kfile=kustomization.yml, got %q", kfile)
	}
}

func TestIsKustomizeDir_ReturnsFileName(t *testing.T) {
	dir := t.TempDir()
	writeKustomizationFile(t, dir, "resources: []\n")
	kfile, ok, err := IsKustomizeDir(dir)
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if kfile != "kustomization.yaml" {
		t.Errorf("expected kfile=kustomization.yaml, got %q", kfile)
	}
}

func TestBuild_Overlay(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize binary not found in PATH")
	}

	// kustomize-simple has a root kustomization.yaml with a patch, making it a
	// self-contained single-overlay fixture suitable for a straightforward build test.
	fixtureDir := filepath.Join("..", "scanner", "testdata", "kustomize-simple")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skipf("fixture directory not found: %s", fixtureDir)
	}

	out, err := Build(fixtureDir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "v9.9.9-patched") {
		t.Errorf("expected rendered output to contain v9.9.9-patched, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "v1.0.0-base") {
		t.Errorf("expected rendered output NOT to contain v1.0.0-base (patch should override), got:\n%s", rendered)
	}
}
