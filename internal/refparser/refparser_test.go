package refparser

import (
	"strings"
	"testing"
)

func TestParse_PlainRef_NoTag(t *testing.T) {
	ref, err := Parse("ghcr.io/stefanprodan/podinfo")
	if err != nil {
		t.Fatalf("Parse plain ref: unexpected error: %v", err)
	}
	if ref.Registry != "ghcr.io" {
		t.Errorf("Registry: got %q, want %q", ref.Registry, "ghcr.io")
	}
	if ref.Repository != "stefanprodan/podinfo" {
		t.Errorf("Repository: got %q, want %q", ref.Repository, "stefanprodan/podinfo")
	}
	// Spec says: does NOT add a default "latest" tag — only returns tag if one was present
	if ref.Tag != "" {
		t.Errorf("Tag: expected empty (no default latest), got %q", ref.Tag)
	}
	if ref.Digest != "" {
		t.Errorf("Digest: expected empty, got %q", ref.Digest)
	}
}

func TestParse_RefWithTag(t *testing.T) {
	ref, err := Parse("ghcr.io/stefanprodan/charts/podinfo:6.13.0")
	if err != nil {
		t.Fatalf("Parse ref with tag: unexpected error: %v", err)
	}
	if ref.Registry != "ghcr.io" {
		t.Errorf("Registry: got %q, want %q", ref.Registry, "ghcr.io")
	}
	if ref.Repository != "stefanprodan/charts/podinfo" {
		t.Errorf("Repository: got %q, want %q", ref.Repository, "stefanprodan/charts/podinfo")
	}
	if ref.Tag != "6.13.0" {
		t.Errorf("Tag: got %q, want %q", ref.Tag, "6.13.0")
	}
}

func TestParse_RefWithDigest(t *testing.T) {
	const digestStr = "sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1"
	raw := "ghcr.io/example/app@" + digestStr
	ref, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse ref with digest: unexpected error: %v", err)
	}
	if ref.Registry != "ghcr.io" {
		t.Errorf("Registry: got %q, want %q", ref.Registry, "ghcr.io")
	}
	if !strings.HasPrefix(ref.Digest, "sha256:") {
		t.Errorf("Digest: expected sha256: prefix, got %q", ref.Digest)
	}
	if ref.Tag != "" {
		t.Errorf("Tag: expected empty for digest-only ref, got %q", ref.Tag)
	}
}

func TestParse_OciPrefixStripped(t *testing.T) {
	const raw = "oci://ghcr.io/example/my-repo:latest"
	ref, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse oci:// prefix: unexpected error: %v", err)
	}
	if ref.Registry != "ghcr.io" {
		t.Errorf("Registry: got %q, want %q", ref.Registry, "ghcr.io")
	}
	if ref.Repository != "example/my-repo" {
		t.Errorf("Repository: got %q, want %q", ref.Repository, "example/my-repo")
	}
	if ref.Tag != "latest" {
		t.Errorf("Tag: got %q, want %q", ref.Tag, "latest")
	}
	if ref.Raw != raw {
		t.Errorf("Raw: got %q, want original string %q", ref.Raw, raw)
	}
}

func TestParse_InvalidRef(t *testing.T) {
	_, err := Parse("not a valid ref!!@@##")
	if err == nil {
		t.Error("Parse invalid ref: expected error, got nil")
	}
}

func TestParse_OciPrefixOnlyNoTag(t *testing.T) {
	ref, err := Parse("oci://ghcr.io/example/myrepo")
	if err != nil {
		t.Fatalf("Parse oci:// no tag: unexpected error: %v", err)
	}
	if ref.Tag != "" {
		t.Errorf("Tag: expected empty (no default latest), got %q", ref.Tag)
	}
}

func TestParse_RawPreserved(t *testing.T) {
	raw := "oci://ghcr.io/stefanprodan/charts/podinfo:6.13.0"
	ref, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse raw preserved: unexpected error: %v", err)
	}
	if ref.Raw != raw {
		t.Errorf("Raw: got %q, want %q", ref.Raw, raw)
	}
}
