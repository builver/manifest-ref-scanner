package helm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsHelmChart_Valid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: my-chart\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, ok, err := IsHelmChart(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true for directory with Chart.yaml")
	}
	if name != "my-chart" {
		t.Errorf("expected name=my-chart, got %q", name)
	}
}

func TestIsHelmChart_NoFile(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := IsHelmChart(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when no Chart.yaml is present")
	}
}

func TestIsHelmChart_NoName_FallsBackToDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, ok, err := IsHelmChart(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if name != filepath.Base(dir) {
		t.Errorf("expected name=%q (dir base), got %q", filepath.Base(dir), name)
	}
}

func TestTemplate(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm binary not found in PATH")
	}

	fixtureDir := filepath.Join("testdata", "helm-test")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", fixtureDir)
	}

	out, err := Template("helm-test", fixtureDir)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "registry.example.com/helm-test:v1.2.3") {
		t.Errorf("expected rendered output to contain known image, got:\n%s", rendered)
	}
}
