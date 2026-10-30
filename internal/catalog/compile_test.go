package catalog_test

import (
	"reflect"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

// The embedded snapshot must match the YAML; if this fails, run
// `go generate ./...` and commit catalog/catalog.bin.
func TestCompiledCatalogIsCurrent(t *testing.T) {
	fromYAML := load(t)
	compiled, err := catalog.LoadCompiled(catalogdata.Compiled, catalogdata.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromYAML, compiled) {
		t.Fatal("catalog/catalog.bin is out of date: run `go generate ./...`")
	}
}
