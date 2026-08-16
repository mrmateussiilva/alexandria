package filesystem_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"alexandria/internal/filesystem"
)

func TestResolveLibraryFileAcceptsFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Go", "livro.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	resolved, info, err := filesystem.ResolveLibraryFile(root, "Go/livro.pdf")
	if err != nil {
		t.Fatalf("resolve file: %v", err)
	}
	if resolved != path {
		t.Fatalf("expected %q, got %q", path, resolved)
	}
	if info.Size() != 3 {
		t.Fatalf("expected size 3, got %d", info.Size())
	}
}

func TestResolveLibraryFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, _, err := filesystem.ResolveLibraryFile(root, "../outside.pdf")
	if !errors.Is(err, filesystem.ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}

func TestResolveLibraryFileRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.pdf")
	if err := os.WriteFile(outsideFile, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "link.pdf")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, _, err := filesystem.ResolveLibraryFile(root, "link.pdf")
	if !errors.Is(err, filesystem.ErrPathEscape) {
		t.Fatalf("expected ErrPathEscape, got %v", err)
	}
}
