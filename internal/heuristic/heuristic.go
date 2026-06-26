package heuristic

import (
	"fmt"
	"regexp"

	"github.com/builver/manifest-ref-scanner/internal/coverage"
	"github.com/builver/manifest-ref-scanner/internal/registry"
)

// ociPattern matches strings that look like OCI references with a qualified
// registry host (must contain a dot or colon to distinguish from bare image
// names like "nginx:latest"). Examples that match:
//   - ghcr.io/myorg/myapp:v1.2.3
//   - registry.example.com/team/image:sha256-abc
//   - localhost:5000/myimage:latest
//
// Deliberately conservative: bare Docker Hub names ("nginx:latest") are
// excluded because they have very high false-positive rates in config files.
var ociPattern = regexp.MustCompile(
	`(?i)\b([a-z0-9][a-z0-9._-]*(\.[a-z]{2,}|:[0-9]+)/[a-z0-9._/\-]+:[a-z0-9._\-]+)\b`,
)

// Scan walks every string value in every resource's Raw map and returns
// strings that match the OCI reference pattern but are not already in knownRefs.
// knownRefs should be the set of canonical Reference values already extracted
// by the resolver so that already-captured artifacts are not reported again.
func Scan(resources []*registry.Resource, knownRefs map[string]bool) []coverage.HeuristicHit {
	var hits []coverage.HeuristicHit
	seen := make(map[string]bool) // value+file dedup

	for _, res := range resources {
		if res.Synthetic {
			continue // synthesized resources have no real source fields to scan
		}
		walkStrings(res.Raw, "", res, knownRefs, seen, &hits)
	}
	return hits
}

// walkStrings recursively visits every string value in v, recording OCI-like
// strings not already in knownRefs.
func walkStrings(
	v any,
	path string,
	res *registry.Resource,
	knownRefs map[string]bool,
	seen map[string]bool,
	hits *[]coverage.HeuristicHit,
) {
	switch val := v.(type) {
	case string:
		for _, match := range ociPattern.FindAllString(val, -1) {
			if knownRefs[match] {
				continue
			}
			key := fmt.Sprintf("%s\x00%s", match, res.SourceFile)
			if seen[key] {
				continue
			}
			seen[key] = true
			*hits = append(*hits, coverage.HeuristicHit{
				Value:     match,
				Kind:      res.Kind,
				Name:      res.Name,
				Namespace: res.Namespace,
				File:      res.SourceFile,
				FieldPath: path,
			})
		}

	case map[string]any:
		for k, child := range val {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			walkStrings(child, childPath, res, knownRefs, seen, hits)
		}

	case []any:
		for i, child := range val {
			walkStrings(child, fmt.Sprintf("%s[%d]", path, i), res, knownRefs, seen, hits)
		}
	}
}
