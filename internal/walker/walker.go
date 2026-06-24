package walker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// ParsedDoc holds a single parsed Kubernetes resource from a YAML file.
type ParsedDoc struct {
	Raw        map[string]interface{}
	SourceFile string
}

// Walk recursively finds all *.yaml / *.yml files under root and parses
// every YAML document within them, returning the flat list of resources.
func Walk(root string) ([]ParsedDoc, error) {
	var docs []ParsedDoc

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories (e.g. .git)
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		fileDocs, err := parseFile(path)
		if err != nil {
			// Non-fatal: warn but continue
			fmt.Fprintf(os.Stderr, "warn: skipping %s: %v\n", path, err)
			return nil
		}
		docs = append(docs, fileDocs...)
		return nil
	})

	return docs, err
}

// parseFile reads a YAML file and splits it into individual documents.
// Each document is unmarshalled into a map and returned as a ParsedDoc.
func parseFile(path string) ([]ParsedDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var docs []ParsedDoc
	dec := newDecoder(data)
	for {
		var raw map[string]interface{}
		chunk, err := dec.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return docs, fmt.Errorf("decode: %w", err)
		}
		if len(bytes.TrimSpace(chunk)) == 0 {
			continue
		}
		if err := yaml.Unmarshal(chunk, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: unmarshal error: %v\n", path, err)
			continue
		}
		if raw == nil {
			continue
		}
		docs = append(docs, ParsedDoc{Raw: raw, SourceFile: path})
	}
	return docs, nil
}

// decoder splits a multi-document YAML stream on "---" boundaries.
type decoder struct {
	data   []byte
	offset int
}

func newDecoder(data []byte) *decoder {
	return &decoder{data: data}
}

func (d *decoder) next() ([]byte, error) {
	if d.offset >= len(d.data) {
		return nil, io.EOF
	}
	rest := d.data[d.offset:]
	// Find the next "---" separator on its own line
	sep := []byte("\n---")
	idx := bytes.Index(rest, sep)
	if idx == -1 {
		d.offset = len(d.data)
		return rest, nil
	}
	chunk := rest[:idx]
	d.offset += idx + len(sep)
	// Skip the rest of the separator line
	if nl := bytes.IndexByte(d.data[d.offset:], '\n'); nl != -1 {
		d.offset += nl + 1
	} else {
		d.offset = len(d.data)
	}
	return chunk, nil
}
