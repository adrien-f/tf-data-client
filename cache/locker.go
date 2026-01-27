package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// Locker manages file-based locks for cache operations.
type Locker struct {
	locksDir string
}

// NewLocker creates a new Locker that stores lock files in the given directory.
func NewLocker(locksDir string) *Locker {
	return &Locker{locksDir: locksDir}
}

// lockPath returns the path to the lock file for a provider.
func (l *Locker) lockPath(id ProviderIdentifier) string {
	// Use flat naming to avoid nested directories
	name := fmt.Sprintf("%s-%s-%s.lock", id.Namespace, id.Name, id.Version)
	// Sanitize path separators and other problematic characters
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "-") // Windows drive letters
	return filepath.Join(l.locksDir, name)
}

// AcquireExclusive acquires an exclusive lock for the given provider.
// The returned function releases the lock and should be called when done.
// Returns an error if the context is cancelled while waiting for the lock.
func (l *Locker) AcquireExclusive(ctx context.Context, id ProviderIdentifier) (unlock func() error, err error) {
	// Ensure locks directory exists
	if err := os.MkdirAll(l.locksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create locks directory: %w", err)
	}

	lockPath := l.lockPath(id)
	fl := flock.New(lockPath)

	// TryLockContext will retry with backoff until context is cancelled or lock is acquired
	locked, err := fl.TryLockContext(ctx, 100_000_000) // 100ms retry interval
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("failed to acquire lock: %v", ctx.Err())
	}

	return fl.Unlock, nil
}
