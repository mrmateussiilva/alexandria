package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathEscape = errors.New("path escapes library root")
	ErrNotRegular = errors.New("path is not a regular file")
)

func ResolveLibraryFile(root, relativePath string) (string, os.FileInfo, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(relativePath) == "" {
		return "", nil, ErrPathEscape
	}

	cleanRel := filepath.Clean(filepath.FromSlash(relativePath))
	if cleanRel == "." || filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", nil, ErrPathEscape
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve library root: %w", err)
	}
	joined := filepath.Join(resolvedRoot, cleanRel)
	resolvedPath, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", nil, fmt.Errorf("resolve library file: %w", err)
	}

	relToRoot, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return "", nil, fmt.Errorf("check library file boundary: %w", err)
	}
	if relToRoot == ".." || filepath.IsAbs(relToRoot) || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", nil, ErrPathEscape
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", nil, fmt.Errorf("stat library file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, ErrNotRegular
	}

	return resolvedPath, info, nil
}
