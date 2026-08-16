package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerPreventsConcurrentScansForSameLibrary(t *testing.T) {
	manager := NewManager(nil, nil)

	if !manager.tryStart("library-1") {
		t.Fatal("expected first scan to start")
	}
	if manager.tryStart("library-1") {
		t.Fatal("expected second scan for same library to be rejected")
	}
	if !manager.tryStart("library-2") {
		t.Fatal("expected scan for different library to start")
	}

	manager.finish("library-1")
	if !manager.tryStart("library-1") {
		t.Fatal("expected scan to start after previous scan finished")
	}
}

func TestFileRecordRejectsPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "escape.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	entry, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if _, ok := fileRecord(root, path, entry[0]); ok {
		t.Fatal("expected path outside root to be rejected")
	}
}
