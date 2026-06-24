package refparser

import (
	"strings"

	"github.com/distribution/reference"
)

type Ref struct {
	Raw        string
	Registry   string
	Repository string
	Tag        string
	Digest     string
}

// Parse parses an OCI reference string, stripping a leading "oci://" prefix if present.
func Parse(raw string) (*Ref, error) {
	s := strings.TrimPrefix(raw, "oci://")
	named, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return nil, err
	}
	r := &Ref{
		Raw:        raw,
		Registry:   reference.Domain(named),
		Repository: reference.Path(named),
	}
	if tagged, ok := named.(reference.Tagged); ok {
		r.Tag = tagged.Tag()
	}
	if digested, ok := named.(reference.Digested); ok {
		r.Digest = digested.Digest().String()
	}
	return r, nil
}
