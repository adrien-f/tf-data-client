package tfclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/infracollect/tf-data-client/cache"
	"github.com/infracollect/tf-data-client/registry"
	"github.com/go-logr/logr"
)

// ProviderConfig identifies a provider. Used as input to CreateProvider/StopProvider
// and returned from Provider.Config() with the actual resolved version.
type ProviderConfig struct {
	Namespace string // e.g., "hashicorp"
	Name      string // e.g., "kubernetes"
	Version   string // CreateProvider: optional (empty = latest). Config(): always resolved version.
}

// String returns a unique key for a provider including version.
// This allows running multiple versions of the same provider simultaneously.
func (c ProviderConfig) String() string {
	return fmt.Sprintf("%s/%s@%s", c.Namespace, c.Name, c.Version)
}

// providerKey returns the map key for a provider by resolved version.
func providerKey(namespace, name, resolvedVersion string) string {
	return fmt.Sprintf("%s/%s@%s", namespace, name, resolvedVersion)
}

// Client orchestrates provider lifecycle management.
type Client struct {
	registry   registry.Registry
	cache      cache.Cache
	logger     logr.Logger
	providers  map[string]*provider   // key = providerKey(ns, name, resolvedVersion)
	latestKeys map[string]string      // "namespace/name" -> resolved key, when created with Version ""
	mu         sync.Mutex
}

// New creates a new Client with the given options.
// If no options are provided, it uses default settings:
// - Filesystem cache at ~/.opentofu-data-client/providers
// - Terraform registry
func New(opts ...Option) (*Client, error) {
	c := &Client{
		providers:  make(map[string]*provider),
		latestKeys: make(map[string]string),
		logger:     logr.Discard(),
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
// The returned Provider.Config() has the actual resolved version (use it for StopProvider if you passed "").
func (c *Client) CreateProvider(ctx context.Context, cfg ProviderConfig) (Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Resolve version if not specified
	version := cfg.Version
	if version == "" {
		latest, err := c.registry.GetLatestVersion(ctx, cfg.Namespace, cfg.Name)
		if err != nil {
			return nil, &ErrProviderNotFound{
				Namespace: cfg.Namespace,
				Name:      cfg.Name,
				Err:       err,
			}
		}
		version = latest
	}

	key := providerKey(cfg.Namespace, cfg.Name, version)

	// Check if provider is already running (match "" or specific version)
	if existing, ok := c.providers[key]; ok {
		if cfg.Version == "" {
			c.latestKeys[cfg.Namespace+"/"+cfg.Name] = key
		}
		return existing, nil
	}

	// Get executable path (from cache or download) using resolved version
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
	c.logger.V(1).Info("launching provider", "namespace", cfg.Namespace, "name", cfg.Name, "version", version, "path", execPath)
	provider, err := launchProvider(execPath, c.logger)
	if err != nil {
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

	provider.namespace = cfg.Namespace
	provider.name = cfg.Name
	provider.version = version

	if err := provider.getSchema(ctx); err != nil {
		provider.Close()
		return nil, &ErrSchemaFailed{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
			Err:       err,
		}
	}

	c.providers[key] = provider
	if cfg.Version == "" {
		c.latestKeys[cfg.Namespace+"/"+cfg.Name] = key
	}
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

	return c.cache.GetOrPut(ctx, id, func(ctx context.Context) (string, func(), error) {
		downloadInfo, err := c.registry.GetDownloadInfo(ctx, namespace, name, version, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get download info: %w", err)
		}

		tmpFile, err := os.CreateTemp("", "provider-*.zip")
		if err != nil {
			return "", nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		cleanup := func() { os.Remove(tmpPath) }

		if err := c.registry.DownloadToPath(ctx, downloadInfo, tmpPath); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("failed to download provider: %w", err)
		}

		return tmpPath, cleanup, nil
	})
}

// StopProvider stops a specific provider by namespace, name, and version.
func (c *Client) StopProvider(ctx context.Context, cfg ProviderConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var key string
	if cfg.Version == "" {
		key = c.latestKeys[cfg.Namespace+"/"+cfg.Name]
	} else {
		key = providerKey(cfg.Namespace, cfg.Name, cfg.Version)
	}

	provider, ok := c.providers[key]
	if !ok {
		return nil
	}

	if err := provider.Close(); err != nil {
		return err
	}

	delete(c.providers, key)
	if cfg.Version == "" {
		delete(c.latestKeys, cfg.Namespace+"/"+cfg.Name)
	}
	return nil
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
	for k := range c.latestKeys {
		delete(c.latestKeys, k)
	}
	return lastErr
}
