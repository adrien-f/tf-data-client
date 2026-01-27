package cache

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FilesystemCache implements Cache using the local filesystem.
type FilesystemCache struct {
	baseDir string
	locker  *Locker
}

// NewFilesystemCache creates a new filesystem-based cache at the given directory.
func NewFilesystemCache(baseDir string) *FilesystemCache {
	locksDir := filepath.Join(baseDir, ".locks")
	return &FilesystemCache{
		baseDir: baseDir,
		locker:  NewLocker(locksDir),
	}
}

// providerDir returns the directory path for a provider.
func (c *FilesystemCache) providerDir(id ProviderIdentifier) string {
	return filepath.Join(c.baseDir, id.Namespace, id.Name, id.Version)
}

// Get retrieves the executable path for a cached provider.
// Returns empty string and nil error if the provider is not cached.
func (c *FilesystemCache) Get(ctx context.Context, id ProviderIdentifier) (string, error) {
	dir := c.providerDir(id)
	execPath := findProviderExecutable(dir, id.Name)
	if execPath != "" {
		if _, err := os.Stat(execPath); err == nil {
			return execPath, nil
		}
	}
	return "", nil
}

// Put stores a provider archive and returns the path to the extracted executable.
func (c *FilesystemCache) Put(ctx context.Context, id ProviderIdentifier, archivePath string) (string, error) {
	dir := c.providerDir(id)

	// Create cache directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Extract the zip file
	if err := extractZip(archivePath, dir); err != nil {
		return "", fmt.Errorf("failed to extract provider: %w", err)
	}

	// Find the executable
	execPath := findProviderExecutable(dir, id.Name)
	if execPath == "" {
		return "", fmt.Errorf("provider executable not found after extraction")
	}

	// Make it executable
	if err := os.Chmod(execPath, 0755); err != nil {
		return "", fmt.Errorf("failed to make provider executable: %w", err)
	}

	return execPath, nil
}

// Has checks if a provider is cached.
func (c *FilesystemCache) Has(ctx context.Context, id ProviderIdentifier) (bool, error) {
	execPath, err := c.Get(ctx, id)
	if err != nil {
		return false, err
	}
	return execPath != "", nil
}

// GetOrPut atomically retrieves a cached provider or invokes downloadFn to populate it.
// This method is safe for concurrent use across multiple processes.
func (c *FilesystemCache) GetOrPut(ctx context.Context, id ProviderIdentifier,
	downloadFn func(ctx context.Context) (archivePath string, cleanup func(), err error)) (string, error) {

	// Acquire exclusive lock for this provider
	unlock, err := c.locker.AcquireExclusive(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to acquire cache lock: %w", err)
	}
	defer unlock()

	// Re-check cache - another process may have populated it while we waited for the lock
	execPath, err := c.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if execPath != "" {
		return execPath, nil
	}

	// Call downloadFn to get the archive
	archivePath, cleanup, err := downloadFn(ctx)
	if err != nil {
		return "", err
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Extract to temp directory for atomic operation
	tmpDir, err := c.createTempDir()
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Extract the zip file to temp directory
	if err := extractZip(archivePath, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to extract provider: %w", err)
	}

	// Find and chmod the executable in temp directory
	execPath = findProviderExecutable(tmpDir, id.Name)
	if execPath == "" {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("provider executable not found after extraction")
	}

	if err := os.Chmod(execPath, 0755); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to make provider executable: %w", err)
	}

	// Create parent directories for final location
	finalDir := c.providerDir(id)
	if err := os.MkdirAll(filepath.Dir(finalDir), 0755); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Atomic rename from temp to final location
	if err := os.Rename(tmpDir, finalDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to move provider to cache: %w", err)
	}

	// Return the executable path in the final location
	return findProviderExecutable(finalDir, id.Name), nil
}

// createTempDir creates a unique temporary directory under the cache's .tmp directory.
func (c *FilesystemCache) createTempDir() (string, error) {
	tmpBase := filepath.Join(c.baseDir, ".tmp")
	if err := os.MkdirAll(tmpBase, 0755); err != nil {
		return "", err
	}

	// Generate random suffix
	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return "", err
	}
	suffix := hex.EncodeToString(randBytes[:])

	tmpDir := filepath.Join(tmpBase, suffix)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}
	return tmpDir, nil
}

// findProviderExecutable finds the provider executable in a directory.
func findProviderExecutable(dir, name string) string {
	// Provider executables follow the pattern terraform-provider-{name}*
	pattern := fmt.Sprintf("terraform-provider-%s*", name)
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// extractZip extracts a zip file to a destination directory.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}
