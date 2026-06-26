package resolver

import (
	"testing"

	"github.com/builver/manifest-ref-scanner/internal/registry"
)

func TestDedup_LongerChainWins(t *testing.T) {
	// Two artifacts with the same raw ref and field type but different chain lengths.
	// The longer chain should be kept.
	shortChain := []*registry.Artifact{
		{
			FieldType: "ociArtifact",
			Reference: "ghcr.io/example/repo:v1",
			Resolution: []registry.ResolutionStep{
				{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
			},
		},
	}
	longChain := []*registry.Artifact{
		{
			FieldType: "ociArtifact",
			Reference: "ghcr.io/example/repo:v1",
			Resolution: []registry.ResolutionStep{
				{Kind: "Kustomization", Name: "infra", Namespace: "flux-system", Via: "spec/sourceRef"},
				{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
			},
		},
	}

	combined := append(shortChain, longChain...)
	result := dedup(combined)

	if len(result) != 1 {
		t.Fatalf("dedup: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Resolution) != 2 {
		t.Errorf("dedup: expected longer chain (2 steps) to win, got %d steps", len(result[0].Resolution))
	}
}

func TestDedup_LongerChainWins_OrderReversed(t *testing.T) {
	// Same test but with longer chain presented first — result should still have the longer chain.
	longChain := &registry.Artifact{
		FieldType: "ociArtifact",
		Reference: "ghcr.io/example/repo:v2",
		Resolution: []registry.ResolutionStep{
			{Kind: "HelmRelease", Name: "hr", Namespace: "apps", Via: "spec/chartRef"},
			{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
		},
	}
	shortChain := &registry.Artifact{
		FieldType: "ociArtifact",
		Reference: "ghcr.io/example/repo:v2",
		Resolution: []registry.ResolutionStep{
			{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
		},
	}

	result := dedup([]*registry.Artifact{longChain, shortChain})

	if len(result) != 1 {
		t.Fatalf("dedup reversed: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Resolution) != 2 {
		t.Errorf("dedup reversed: expected longer chain (2 steps) to win, got %d steps", len(result[0].Resolution))
	}
}

func TestDedup_DifferentRefs_BothKept(t *testing.T) {
	arts := []*registry.Artifact{
		{
			FieldType: "ociArtifact",
			Reference: "ghcr.io/example/repo-a:v1",
			Resolution: []registry.ResolutionStep{
				{Kind: "OCIRepository", Name: "repo-a"},
			},
		},
		{
			FieldType: "ociArtifact",
			Reference: "ghcr.io/example/repo-b:v2",
			Resolution: []registry.ResolutionStep{
				{Kind: "OCIRepository", Name: "repo-b"},
			},
		},
	}

	result := dedup(arts)
	if len(result) != 2 {
		t.Errorf("dedup different refs: expected 2 artifacts, got %d", len(result))
	}
}

func TestDedup_SameRawDifferentFieldType_BothKept(t *testing.T) {
	// Same raw URL but different field types are treated as distinct artifacts.
	arts := []*registry.Artifact{
		{
			FieldType: "ociArtifact",
			Reference: "ghcr.io/example/repo:v1",
			Resolution: []registry.ResolutionStep{
				{Kind: "OCIRepository", Name: "repo"},
			},
		},
		{
			FieldType: "containerImage",
			Reference: "ghcr.io/example/repo:v1",
			Resolution: []registry.ResolutionStep{
				{Kind: "Deployment", Name: "my-deploy"},
			},
		},
	}

	result := dedup(arts)
	if len(result) != 2 {
		t.Errorf("dedup different fieldType: expected 2 artifacts, got %d", len(result))
	}
}

func TestDedup_Empty(t *testing.T) {
	result := dedup(nil)
	if len(result) != 0 {
		t.Errorf("dedup empty: expected empty result, got %v", result)
	}
}

func TestDedup_PreservesInsertionOrder(t *testing.T) {
	// First-seen ref should appear before second-seen ref.
	arts := []*registry.Artifact{
		{FieldType: "ociArtifact", Reference: "ghcr.io/example/first:v1",
			Resolution: []registry.ResolutionStep{{Kind: "OCIRepository", Name: "first"}}},
		{FieldType: "ociArtifact", Reference: "ghcr.io/example/second:v2",
			Resolution: []registry.ResolutionStep{{Kind: "OCIRepository", Name: "second"}}},
	}
	result := dedup(arts)
	if len(result) != 2 {
		t.Fatalf("dedup order: expected 2 results, got %d", len(result))
	}
	if result[0].Reference != "ghcr.io/example/first:v1" {
		t.Errorf("dedup order: expected first entry to be 'first', got %q", result[0].Reference)
	}
	if result[1].Reference != "ghcr.io/example/second:v2" {
		t.Errorf("dedup order: expected second entry to be 'second', got %q", result[1].Reference)
	}
}
