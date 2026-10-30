package library_test

import (
	"path/filepath"
	"testing"

	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func TestCustomCatalog(t *testing.T) {
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, body := range []string{`{"name":"a"}`, `{"name":"b"}`} {
		if err := db.AddCustom(library.CustomEntry{Kind: "cable", ID: "usb-a:m|usb-c:m#mine", Body: []byte(body)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.CustomCatalog()
	if err != nil || len(got) != 1 || string(got[0].Body) != `{"name":"b"}` {
		t.Fatalf("CustomCatalog() = %+v (err %v)", got, err)
	}
}
