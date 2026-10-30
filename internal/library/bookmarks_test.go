package library_test

import (
	"path/filepath"
	"testing"

	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func TestBookmarks(t *testing.T) {
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a, err := db.AddBookmark("Retro", "PS2", "CRT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddBookmark("", "Laptop", "Monitor"); err != nil {
		t.Fatal(err)
	}
	got, err := db.Bookmarks()
	if err != nil || len(got) != 2 || got[0].Name != "Retro" || got[0].From != "PS2" || got[1].To != "Monitor" {
		t.Fatalf("Bookmarks() = %+v (err %v)", got, err)
	}
	if err := db.RemoveBookmark(a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.Bookmarks(); len(got) != 1 || got[0].From != "Laptop" {
		t.Fatalf("after remove: %+v", got)
	}
	if err := db.RemoveBookmark(a.ID); err == nil {
		t.Error("removing twice should fail")
	}
}
