package resolver

import (
	"testing"

	"github.com/builver/manifest-ref-scanner/internal/registry"
)

func art(ref, fieldType string, chain ...registry.ResolutionStep) *registry.Artifact {
	return &registry.Artifact{
		FieldType: fieldType,
		Reference: ref,
		Sources:   []registry.ArtifactSource{{Chain: chain}},
	}
}

func artWithOverlay(ref, fieldType, overlay string, chain ...registry.ResolutionStep) *registry.Artifact {
	a := art(ref, fieldType, chain...)
	a.KustomizeOverlays = []string{overlay}
	return a
}

func TestDedup_SameRef_UniqueChains_BothKept(t *testing.T) {
	// Two artifacts with the same ref+fieldType but different resolution chains.
	// Both chains must appear in Sources.
	arts := []*registry.Artifact{
		art("ghcr.io/example/repo:v1", "ociArtifact",
			registry.ResolutionStep{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
		),
		art("ghcr.io/example/repo:v1", "ociArtifact",
			registry.ResolutionStep{Kind: "Kustomization", Name: "infra", Namespace: "flux-system", Via: "spec/sourceRef"},
			registry.ResolutionStep{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"},
		),
	}

	result := dedup(arts)

	if len(result) != 1 {
		t.Fatalf("dedup: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Sources) != 2 {
		t.Errorf("dedup: expected 2 sources, got %d", len(result[0].Sources))
	}
}

func TestDedup_SameRef_IdenticalChains_OnlyOneKept(t *testing.T) {
	// Two artifacts with identical resolution chains must collapse to one source.
	chain := []registry.ResolutionStep{{Kind: "OCIRepository", Name: "repo", Namespace: "flux-system"}}
	arts := []*registry.Artifact{
		{FieldType: "ociArtifact", Reference: "ghcr.io/example/repo:v1", Sources: []registry.ArtifactSource{{Chain: chain}}},
		{FieldType: "ociArtifact", Reference: "ghcr.io/example/repo:v1", Sources: []registry.ArtifactSource{{Chain: chain}}},
	}

	result := dedup(arts)

	if len(result) != 1 {
		t.Fatalf("dedup: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Sources) != 1 {
		t.Errorf("dedup: expected 1 source for identical chains, got %d", len(result[0].Sources))
	}
}

func TestDedup_DifferentRefs_BothKept(t *testing.T) {
	arts := []*registry.Artifact{
		art("ghcr.io/example/repo-a:v1", "ociArtifact", registry.ResolutionStep{Kind: "OCIRepository", Name: "repo-a"}),
		art("ghcr.io/example/repo-b:v2", "ociArtifact", registry.ResolutionStep{Kind: "OCIRepository", Name: "repo-b"}),
	}

	result := dedup(arts)
	if len(result) != 2 {
		t.Errorf("dedup different refs: expected 2 artifacts, got %d", len(result))
	}
}

func TestDedup_SameRawDifferentFieldType_BothKept(t *testing.T) {
	arts := []*registry.Artifact{
		art("ghcr.io/example/repo:v1", "ociArtifact", registry.ResolutionStep{Kind: "OCIRepository", Name: "repo"}),
		art("ghcr.io/example/repo:v1", "containerImage", registry.ResolutionStep{Kind: "Deployment", Name: "my-deploy"}),
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

func TestDedup_SameRefFromInlineAndRealDeployment_BothSourcesPresent(t *testing.T) {
	// Same image in a real Deployment (deploy.yaml) and an inline-expanded Deployment
	// (composition.yaml). Result: 1 artifact, 2 sources — both chains visible.
	arts := []*registry.Artifact{
		art("ghcr.io/example/app:v1", "containerImage",
			registry.ResolutionStep{Kind: "Deployment", Name: "my-app", Namespace: "default", File: "deploy.yaml", Inline: false},
		),
		art("ghcr.io/example/app:v1", "containerImage",
			registry.ResolutionStep{Kind: "Deployment", Name: "my-app", Namespace: "default", File: "composition.yaml", Inline: true},
		),
	}

	result := dedup(arts)
	if len(result) != 1 {
		t.Fatalf("dedup inline+real: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Sources) != 2 {
		t.Fatalf("dedup inline+real: expected 2 sources, got %d", len(result[0].Sources))
	}
	// Second source must be the inline one.
	if !result[0].Sources[1].Chain[0].Inline {
		t.Errorf("dedup inline+real: expected second source chain to have inline=true step")
	}
}

func TestDedup_KustomizeOverlays_StillMerged(t *testing.T) {
	// Same image from two kustomize overlays: chains are identical so one source,
	// but both overlay dirs must be recorded in KustomizeOverlays.
	arts := []*registry.Artifact{
		artWithOverlay("ghcr.io/example/app:v1", "containerImage", "overlays/prod",
			registry.ResolutionStep{Kind: "Deployment", Name: "my-app", File: "base/deployment.yaml"},
		),
		artWithOverlay("ghcr.io/example/app:v1", "containerImage", "overlays/staging",
			registry.ResolutionStep{Kind: "Deployment", Name: "my-app", File: "base/deployment.yaml"},
		),
	}

	result := dedup(arts)
	if len(result) != 1 {
		t.Fatalf("dedup kustomize: expected 1 artifact, got %d", len(result))
	}
	if len(result[0].Sources) != 1 {
		t.Errorf("dedup kustomize: identical chains should produce 1 source, got %d", len(result[0].Sources))
	}
	if len(result[0].KustomizeOverlays) != 2 {
		t.Errorf("dedup kustomize: expected 2 overlay dirs, got %v", result[0].KustomizeOverlays)
	}
}

func TestDedup_PreservesInsertionOrder(t *testing.T) {
	arts := []*registry.Artifact{
		art("ghcr.io/example/first:v1", "ociArtifact", registry.ResolutionStep{Kind: "OCIRepository", Name: "first"}),
		art("ghcr.io/example/second:v2", "ociArtifact", registry.ResolutionStep{Kind: "OCIRepository", Name: "second"}),
	}
	result := dedup(arts)
	if len(result) != 2 {
		t.Fatalf("dedup order: expected 2 results, got %d", len(result))
	}
	if result[0].Reference != "ghcr.io/example/first:v1" {
		t.Errorf("dedup order: expected first entry 'first', got %q", result[0].Reference)
	}
	if result[1].Reference != "ghcr.io/example/second:v2" {
		t.Errorf("dedup order: expected second entry 'second', got %q", result[1].Reference)
	}
}
