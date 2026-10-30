package catalog

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io/fs"
	"maps"
	"slices"
)

// Compile parses the YAML catalog in fsys into a binary snapshot (gob) and
// checks the snapshot loads. `go generate` writes it next to the YAML, and
// the CLI embeds it, so start-up skips YAML parsing: about 3× faster on a
// large catalog.
func Compile(fsys fs.FS) ([]byte, error) {
	raw, err := parseYAML(fsys)
	if err != nil {
		return nil, err
	}
	// Gob writes maps in random order; a sorted list makes the snapshot
	// byte-for-byte reproducible, so CI can check it's current with git diff.
	snap := snapshot{File: raw}
	for _, k := range slices.Sorted(maps.Keys(raw.Glyphs)) {
		snap.Glyphs = append(snap.Glyphs, namedGlyph{k, raw.Glyphs[k]})
	}
	snap.File.Glyphs = nil
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snap); err != nil {
		return nil, err
	}
	if _, err := LoadCompiled(buf.Bytes(), "check"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// LoadCompiled builds the catalog from a snapshot made by Compile, plus any
// custom entries from your library.
func LoadCompiled(data []byte, version string, custom ...Custom) (*Catalog, error) {
	var snap snapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snap); err != nil {
		return nil, fmt.Errorf("compiled catalog: %w (run go generate ./...)", err)
	}
	snap.File.Glyphs = map[string]rawGlyph{}
	for _, g := range snap.Glyphs {
		snap.File.Glyphs[g.Key] = g.Glyph
	}
	for _, cu := range custom {
		snap.File = snap.File.with(cu)
	}
	return build(snap.File, version)
}

// snapshot is the parsed YAML with its glyph map stored as a sorted list.
// Fields are exported because gob skips the rest.
type snapshot struct {
	File   rawFile
	Glyphs []namedGlyph
}

type namedGlyph struct {
	Key   string
	Glyph rawGlyph
}
