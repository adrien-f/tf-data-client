package tfclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/adrien-f/tf-data-client/cache"
	"github.com/adrien-f/tf-data-client/registry"
	"github.com/go-logr/logr"
)

// ProviderConfig specifies which provider to create.
type ProviderConfig struct {
	Namespace string // e.g., "hashicorp"
	Name      string // e.g., "kubernetes"
	Version   string // Optional: specific version (e.g., "2.25.0"), empty = latest
}

// providerKey returns a unique key for a provider.
func (c ProviderConfig) providerKey() string {
	return fmt.Sprintf("%s/%s", c.Namespace, c.Name)
}

// Client orchestrates provider lifecycle management.
type Client struct {
	registry  registry.Registry
	cache     cache.Cache
	logger    logr.Logger
	providers map[string]*Provider
	mu        sync.Mutex
}

// New creates a new Client with the given options.
// If no options are provided, it uses default settings:
// - Filesystem cache at ~/.opentofu-data-client/providers
// - Terraform registry
func New(opts ...Option) (*Client, error) {
	c := &Client{
		providers: make(map[string]*Provider),
		logger:    logr.Discard(),
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if c.registry == nil {
		c.registry = registry.NewTerraformRegistry(nil)
	}

	if c.cache == nil {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir := filepath.Join(homeDir, ".tf-data-client", "providers")
		c.cache = cache.NewFilesystemCache(cacheDir)
	}

	return c, nil
}

// CreateProvider downloads (if needed), launches, and fetches schema for a provider.
// If cfg.Version is empty, fetches and uses the latest version from registry.
func (c *Client) CreateProvider(ctx context.Context, cfg ProviderConfig) (*Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cfg.providerKey()

	// Check if provider is already running
	if existing, ok := c.providers[key]; ok {
		return existing, nil
	}

	// Resolve version if not specified
	version := cfg.Version
	if version == "" {
		var err error
		version, err = c.registry.GetLatestVersion(ctx, cfg.Namespace, cfg.Name)
		if err != nil {
			return nil, &ErrProviderNotFound{
				Namespace: cfg.Namespace,
				Name:      cfg.Name,
			}
		}
	}

	// Get executable path (from cache or download)
	execPath, err := c.getOrDownloadProvider(ctx, cfg.Namespace, cfg.Name, version)
	if err != nil {
		return nil, &ErrDownloadFailed{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
			Version:   version,
			Err:       err,
		}
	}

	// Launch provider
	c.logger.Info("launching provider", "namespace", cfg.Namespace, "name", cfg.Name, "version", version, "path", execPath)
	provider, err := launchProvider(execPath, c.logger)
	if err != nil {
		// Check for protocol version mismatch
		var pm *errProtocolMismatch
		if errors.As(err, &pm) {
			return nil, &ErrProtocolUnsupported{
				Namespace:       cfg.Namespace,
				Name:            cfg.Name,
				Version:         version,
				ProviderVersion: pm.pluginVersion,
				ClientVersion:   pm.clientVersion,
			}
		}
		return nil, &ErrLaunchFailed{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
			Version:   version,
			Err:       err,
		}
	}

	// Set metadata
	provider.Namespace = cfg.Namespace
	provider.Name = cfg.Name
	provider.Version = version

	// Get schema
	if err := provider.getSchema(ctx); err != nil {
		provider.Close()
		return nil, &ErrSchemaFailed{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
			Err:       err,
		}
	}

	c.providers[key] = provider
	return provider, nil
}

// getOrDownloadProvider returns the path to a provider executable,
// downloading it first if not cached.
func (c *Client) getOrDownloadProvider(ctx context.Context, namespace, name, version string) (string, error) {
	id := cache.ProviderIdentifier{
		Namespace: namespace,
		Name:      name,
		Version:   version,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	// Check cache
	execPath, err := c.cache.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("cache lookup failed: %w", err)
	}
	if execPath != "" {
		return execPath, nil
	}

	// Get download info
	downloadInfo, err := c.registry.GetDownloadInfo(ctx, namespace, name, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", fmt.Errorf("failed to get download info: %w", err)
	}

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "provider-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := c.registry.DownloadToPath(ctx, downloadInfo, tmpPath); err != nil {
		return "", fmt.Errorf("failed to download provider: %w", err)
	}

	// Extract to cache
	execPath, err = c.cache.Put(ctx, id, tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to cache provider: %w", err)
	}

	return execPath, nil
}

// StopProvider stops a specific provider by namespace and name.
func (c *Client) StopProvider(ctx context.Context, namespace, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	provider, ok := c.providers[key]
	if !ok {
		return nil
	}

	delete(c.providers, key)
	return provider.Close()
}

// Close stops all running providers.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for key, provider := range c.providers {
		if err := provider.Close(); err != nil {
			lastErr = err
		}
		delete(c.providers, key)
	}
	return lastErr
}
