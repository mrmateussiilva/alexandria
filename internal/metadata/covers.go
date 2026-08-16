package metadata

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"alexandria/internal/books"
	"alexandria/internal/filesystem"
)

var ErrNoLocalCover = errors.New("local cover not found")

func (w *Worker) extractLocalCover(ctx context.Context, location books.FileLocation) (string, error) {
	if w.cachePath == "" {
		return "", fmt.Errorf("cover cache path is not configured")
	}

	filePath, _, err := filesystem.ResolveLibraryFile(location.LibraryPath, location.RelativePath)
	if err != nil {
		return "", err
	}

	if coverPath, err := w.extractSidecarCover(location); err == nil {
		return coverPath, nil
	} else if !errors.Is(err, ErrNoLocalCover) {
		w.logger.Warn("extract sidecar cover", "book_id", location.ID, "error", err)
	}

	switch strings.ToLower(location.Extension) {
	case ".epub":
		return w.extractEPUBCover(location.ID, filePath)
	case ".pdf":
		return w.renderPDFCover(ctx, location.ID, filePath)
	default:
		return "", ErrNoLocalCover
	}
}

func (w *Worker) extractSidecarCover(location books.FileLocation) (string, error) {
	dir := path.Dir(strings.ReplaceAll(location.RelativePath, "\\", "/"))
	if dir == "." {
		dir = ""
	}
	name := strings.TrimSuffix(path.Base(location.RelativePath), path.Ext(location.RelativePath))
	candidates := []string{
		name + ".jpg",
		name + ".jpeg",
		name + ".png",
		name + ".webp",
		"cover.jpg",
		"cover.jpeg",
		"cover.png",
		"folder.jpg",
		"folder.jpeg",
		"folder.png",
	}

	for _, candidate := range candidates {
		relativePath := candidate
		if dir != "" {
			relativePath = path.Join(dir, candidate)
		}
		filePath, _, err := filesystem.ResolveLibraryFile(location.LibraryPath, relativePath)
		if err != nil {
			continue
		}
		return w.copyCoverFile(location.ID, filePath, filepath.Ext(filePath))
	}
	return "", ErrNoLocalCover
}

func (w *Worker) extractEPUBCover(bookID, filePath string) (string, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("open epub: %w", err)
	}
	defer reader.Close()

	byName := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		byName[file.Name] = file
	}

	rootFile, err := epubRootFile(byName)
	if err != nil {
		return "", err
	}
	opfFile := byName[rootFile]
	if opfFile == nil {
		return "", ErrNoLocalCover
	}
	opf, err := readEPUBPackage(opfFile)
	if err != nil {
		return "", err
	}

	coverName := epubCoverName(path.Dir(rootFile), opf)
	if coverName == "" {
		return "", ErrNoLocalCover
	}
	cover := byName[coverName]
	if cover == nil {
		return "", ErrNoLocalCover
	}
	ext := coverExtension(cover.Name, coverMediaType(opf, coverName))
	return w.copyZipCover(bookID, cover, ext)
}

func epubRootFile(files map[string]*zip.File) (string, error) {
	container := files["META-INF/container.xml"]
	if container == nil {
		return "", ErrNoLocalCover
	}
	handle, err := container.Open()
	if err != nil {
		return "", fmt.Errorf("open epub container: %w", err)
	}
	defer handle.Close()

	var parsed epubContainer
	if err := xml.NewDecoder(io.LimitReader(handle, 256*1024)).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode epub container: %w", err)
	}
	for _, rootFile := range parsed.Rootfiles {
		if rootFile.FullPath != "" {
			cleaned := cleanZipPath(rootFile.FullPath)
			if cleaned != "" {
				return cleaned, nil
			}
		}
	}
	return "", ErrNoLocalCover
}

func readEPUBPackage(file *zip.File) (epubPackage, error) {
	handle, err := file.Open()
	if err != nil {
		return epubPackage{}, fmt.Errorf("open epub package: %w", err)
	}
	defer handle.Close()

	var parsed epubPackage
	if err := xml.NewDecoder(io.LimitReader(handle, 2*1024*1024)).Decode(&parsed); err != nil {
		return epubPackage{}, fmt.Errorf("decode epub package: %w", err)
	}
	return parsed, nil
}

func epubCoverName(opfDir string, opf epubPackage) string {
	byID := make(map[string]epubManifestItem, len(opf.Manifest.Items))
	for _, item := range opf.Manifest.Items {
		byID[item.ID] = item
		if strings.Contains(item.Properties, "cover-image") {
			return resolveZipPath(opfDir, item.Href)
		}
	}
	for _, meta := range opf.Metadata.Metas {
		if strings.EqualFold(meta.Name, "cover") {
			if item, ok := byID[meta.Content]; ok {
				return resolveZipPath(opfDir, item.Href)
			}
		}
	}
	for _, item := range opf.Manifest.Items {
		if strings.Contains(item.MediaType, "image/") && strings.Contains(strings.ToLower(item.Href), "cover") {
			return resolveZipPath(opfDir, item.Href)
		}
	}
	for _, item := range opf.Manifest.Items {
		if strings.Contains(item.MediaType, "image/") {
			return resolveZipPath(opfDir, item.Href)
		}
	}
	return ""
}

func coverMediaType(opf epubPackage, fileName string) string {
	for _, item := range opf.Manifest.Items {
		if strings.HasSuffix(fileName, path.Clean(item.Href)) {
			return item.MediaType
		}
	}
	return ""
}

func resolveZipPath(baseDir, href string) string {
	href = strings.TrimSpace(strings.Split(href, "#")[0])
	if href == "" || strings.HasPrefix(href, "/") {
		return ""
	}
	if baseDir == "." {
		baseDir = ""
	}
	return cleanZipPath(path.Join(baseDir, href))
}

func cleanZipPath(value string) string {
	cleaned := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}

func coverExtension(fileName, mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func (w *Worker) renderPDFCover(ctx context.Context, bookID, filePath string) (string, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return "", ErrNoLocalCover
	}
	dir := filepath.Join(w.cachePath, "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cover cache directory: %w", err)
	}

	renderCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	prefix := filepath.Join(dir, bookID+"-pdf")
	generated := prefix + ".jpg"
	os.Remove(generated)

	cmd := exec.CommandContext(
		renderCtx,
		"pdftoppm",
		"-f", "1",
		"-l", "1",
		"-singlefile",
		"-jpeg",
		"-r", "96",
		filePath,
		prefix,
	)
	output, err := cmd.CombinedOutput()
	if renderCtx.Err() != nil {
		return "", fmt.Errorf("render pdf cover timeout: %w", renderCtx.Err())
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("render pdf cover: %s", message)
	}
	if err := validateCoverFile(generated); err != nil {
		os.Remove(generated)
		return "", err
	}

	relativePath := filepath.Join("covers", bookID+".jpg")
	finalPath := filepath.Join(w.cachePath, relativePath)
	if err := os.Rename(generated, finalPath); err != nil {
		return "", fmt.Errorf("move rendered cover: %w", err)
	}
	return relativePath, nil
}

func (w *Worker) copyCoverFile(bookID, sourcePath, ext string) (string, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open local cover: %w", err)
	}
	defer file.Close()
	return w.writeCover(bookID, file, ext)
}

func (w *Worker) copyZipCover(bookID string, source *zip.File, ext string) (string, error) {
	handle, err := source.Open()
	if err != nil {
		return "", fmt.Errorf("open epub cover: %w", err)
	}
	defer handle.Close()
	return w.writeCover(bookID, handle, ext)
}

func (w *Worker) writeCover(bookID string, source io.Reader, ext string) (string, error) {
	if ext == "" {
		ext = ".jpg"
	}
	dir := filepath.Join(w.cachePath, "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create cover cache directory: %w", err)
	}
	relativePath := filepath.Join("covers", bookID+coverExtension("cover"+ext, ""))
	path := filepath.Join(w.cachePath, relativePath)
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create cover cache file: %w", err)
	}
	defer file.Close()

	limited := io.LimitReader(source, maxCoverBytes+1)
	written, err := io.Copy(file, limited)
	if err != nil {
		return "", fmt.Errorf("write cover cache file: %w", err)
	}
	if written > maxCoverBytes {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("cover is larger than %d bytes", maxCoverBytes)
	}
	if err := validateCoverFile(path); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	return relativePath, nil
}

func validateCoverFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat cover cache file: %w", err)
	}
	if info.Size() == 0 {
		return ErrNoLocalCover
	}
	if info.Size() > maxCoverBytes {
		return fmt.Errorf("cover is larger than %d bytes", maxCoverBytes)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open cover cache file: %w", err)
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read cover cache file: %w", err)
	}
	contentType := http.DetectContentType(buffer[:n])
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return nil
	default:
		return fmt.Errorf("unsupported cover content type %q", contentType)
	}
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubPackage struct {
	Metadata struct {
		Metas []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []epubManifestItem `xml:"item"`
	} `xml:"manifest"`
}

type epubManifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}
