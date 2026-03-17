// Package bundle handles extraction and parsing of Kubernetes support bundles.
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MaxFileSizeBytes is the maximum allowed size for a single extracted file.
	MaxFileSizeBytes int64 = 50 * 1024 * 1024 // 50MB

	// MaxTotalSizeBytes is the maximum allowed total size across all extracted files.
	MaxTotalSizeBytes int64 = 500 * 1024 * 1024 // 500MB
)

// Extract streams a gzip-compressed tar archive from r into a temporary
// directory, enforcing per-file and total size limits. It returns the path to
// the temp directory on success. The caller is responsible for removing the
// directory when done (e.g. via defer os.RemoveAll).
//
// maxBytes limits the total decompressed size written to disk. Pass
// MaxTotalSizeBytes for the production default.
func Extract(ctx context.Context, r io.Reader, maxBytes int64) (string, error) {
	tmpDir, err := os.MkdirTemp("", "autopsy-bundle-*")
	if err != nil {
		return "", fmt.Errorf("Extract: create temp dir: %w", err)
	}

	if err := extract(ctx, r, tmpDir, maxBytes); err != nil {
		// Clean up on failure so callers don't have to track partial extractions.
		os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}

func extract(ctx context.Context, r io.Reader, destDir string, maxBytes int64) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("extract: gzip.NewReader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	var totalBytes int64
	fileCount := 0

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("extract: %w", ctx.Err())
		default:
		}

		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("extract: tar.Next: %w", err)
		}

		cleanName, err := sanitizePath(header.Name)
		if err != nil {
			slog.Warn("extract: skipping unsafe path", "path", header.Name, "err", err)
			continue
		}

		destPath := filepath.Join(destDir, cleanName)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o750); err != nil {
				return fmt.Errorf("extract: mkdir %s: %w", destPath, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			// Ensure parent directory exists (some tarballs omit dir entries).
			if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
				return fmt.Errorf("extract: mkdir parent for %s: %w", destPath, err)
			}

			n, err := writeFile(ctx, destPath, tr, MaxFileSizeBytes)
			if err != nil {
				return fmt.Errorf("extract: write %s: %w", cleanName, err)
			}

			totalBytes += n
			if totalBytes > maxBytes {
				return fmt.Errorf("extract: total extracted size exceeds limit (%d bytes)", maxBytes)
			}
			fileCount++

		default:
			// Skip symlinks, hard links, and device files for safety.
		}
	}

	slog.Info("bundle extracted", "dir", destDir, "files", fileCount, "bytes", totalBytes)
	return nil
}

// writeFile copies from r into a new file at path, limited to maxSize bytes.
// Returns the number of bytes written.
func writeFile(ctx context.Context, path string, r io.Reader, maxSize int64) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, fmt.Errorf("writeFile: open: %w", err)
	}
	defer f.Close()

	lr := io.LimitReader(r, maxSize+1)
	n, err := io.Copy(f, lr)
	if err != nil {
		return n, fmt.Errorf("writeFile: copy: %w", err)
	}
	if n > maxSize {
		return n, fmt.Errorf("writeFile: file exceeds %d byte limit", maxSize)
	}
	return n, nil
}

// sanitizePath cleans and validates a tar entry path to prevent path traversal attacks.
// Returns an error if the path is unsafe (contains ".." components or is absolute).
func sanitizePath(name string) (string, error) {
	// Reject paths that start with a slash before any transformation.
	// Tar archives from Unix may use "/" as a leading separator.
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "", fmt.Errorf("path %q is absolute", name)
	}

	// Normalize separators and clean.
	name = filepath.FromSlash(name)
	clean := filepath.Clean(name)

	// Reject any path that resolves upward.
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("path %q contains traversal component", name)
	}

	// Reject absolute paths (covers Windows drive-letter paths like C:\...).
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path %q is absolute after cleaning", name)
	}

	return clean, nil
}
