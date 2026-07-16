package branding

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	dataDir            = "data"
	defaultFaviconPath = "internal/server/public/favicon.svg"
	maxFaviconBytes    = 256 * 1024
)

var allowedExtensions = map[string]string{
	".ico": "image/x-icon",
	".png": "image/png",
	".svg": "image/svg+xml",
}

// MaxFaviconBytes is the maximum allowed favicon upload size.
func MaxFaviconBytes() int {
	return maxFaviconBytes
}

// CustomFaviconPath returns the on-disk path for a custom favicon with the given extension.
func CustomFaviconPath(ext string) string {
	return filepath.Join(dataDir, "custom-favicon"+ext)
}

// DefaultFaviconPath returns the built-in default favicon path.
func DefaultFaviconPath() string {
	return defaultFaviconPath
}

// ValidateAndDetect checks magic bytes and returns the extension and MIME type.
func ValidateAndDetect(data []byte, filename string) (ext, mime string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("empty file")
	}
	if len(data) > maxFaviconBytes {
		return "", "", fmt.Errorf("file too large (max %d KB)", maxFaviconBytes/1024)
	}

	extFromName := strings.ToLower(filepath.Ext(filename))
	detectedExt, detectedMIME := sniffFavicon(data)
	if detectedExt == "" {
		return "", "", fmt.Errorf("unsupported file type; use ICO, PNG, or SVG")
	}
	if extFromName != "" && extFromName != detectedExt {
		return "", "", fmt.Errorf("file content does not match extension")
	}
	return detectedExt, detectedMIME, nil
}

func sniffFavicon(data []byte) (ext, mime string) {
	if len(data) >= 8 && data[0] == 0x89 && bytes.Equal(data[1:4], []byte("PNG")) {
		return ".png", "image/png"
	}
	if len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 {
		return ".ico", "image/x-icon"
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.HasPrefix(trimmed, []byte("<svg")) {
		return ".svg", "image/svg+xml"
	}
	if bytes.Contains(trimmed, []byte("<svg")) {
		return ".svg", "image/svg+xml"
	}
	return "", ""
}

// Save writes the favicon data to disk, removing a stale file if the extension changed.
func Save(data []byte, ext string) error {
	if _, ok := allowedExtensions[ext]; !ok {
		return fmt.Errorf("unsupported extension %q", ext)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	for otherExt := range allowedExtensions {
		if otherExt == ext {
			continue
		}
		_ = os.Remove(CustomFaviconPath(otherExt))
	}
	target := CustomFaviconPath(ext)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("failed to write favicon: %w", err)
	}
	return nil
}

// Delete removes all custom favicon files from disk.
func Delete() error {
	var firstErr error
	for ext := range allowedExtensions {
		if err := os.Remove(CustomFaviconPath(ext)); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Resolve returns the path and MIME type for the active favicon.
func Resolve(ext string) (path, mime string) {
	if ext != "" {
		if mimeType, ok := allowedExtensions[ext]; ok {
			customPath := CustomFaviconPath(ext)
			if _, err := os.Stat(customPath); err == nil {
				return customPath, mimeType
			}
		}
	}
	return defaultFaviconPath, "image/svg+xml"
}

// CacheBust returns a cache-busting token for the favicon URL.
func CacheBust(ext string, fallback string) string {
	if ext == "" {
		return fallback
	}
	customPath := CustomFaviconPath(ext)
	info, err := os.Stat(customPath)
	if err != nil {
		return fallback
	}
	return fmt.Sprintf("%d", info.ModTime().Unix())
}
