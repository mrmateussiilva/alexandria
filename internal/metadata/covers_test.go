package metadata

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func TestExtractEPUBCover(t *testing.T) {
	root := t.TempDir()
	epubPath := filepath.Join(root, "book.epub")
	writeTestEPUB(t, epubPath)

	worker := &Worker{cachePath: root}
	relativePath, err := worker.extractEPUBCover("book-id", epubPath)
	if err != nil {
		t.Fatalf("extract epub cover: %v", err)
	}
	if relativePath != filepath.Join("covers", "book-id.png") {
		t.Fatalf("unexpected cover path %q", relativePath)
	}
	if _, err := os.Stat(filepath.Join(root, relativePath)); err != nil {
		t.Fatalf("expected cached cover: %v", err)
	}
}

func writeTestEPUB(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create epub: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	defer writer.Close()
	writeZipFile(t, writer, "META-INF/container.xml", []byte(`<?xml version="1.0"?>
<container>
  <rootfiles>
    <rootfile full-path="OPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`))
	writeZipFile(t, writer, "OPS/content.opf", []byte(`<?xml version="1.0"?>
<package>
  <manifest>
    <item id="cover" href="images/cover.png" media-type="image/png" properties="cover-image"/>
  </manifest>
</package>`))
	writeZipFile(t, writer, "OPS/images/cover.png", tinyPNG)
}

func writeZipFile(t *testing.T, writer *zip.Writer, name string, body []byte) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
}
