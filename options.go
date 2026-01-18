package tfclient

import (
	"net/http"

	"github.com/adrien-f/tf-data-client/cache"
	"github.com/adrien-f/tf-data-client/registry"
	"github.com/go-logr/logr"
)

// Option configures a Client.
type Option func(*Client) error

// WithLogger sets a custom logger for the client and providers.
// If not set, logging is disabled (logr.Discard() is used).
func WithLogger(logger logr.Logger) Option {
	return func(cl *Client) error {
		cl.logger = logger
		return nil
	}
}

// WithCache sets a custom cache implementation.
func WithCache(c cache.Cache) Option {
	return func(cl *Client) error {
		cl.cache = c
		return nil
	}
}

// WithCacheDir sets the filesystem cache directory.
func WithCacheDir(dir string) Option {
	return func(cl *Client) error {
		cl.cache = cache.NewFilesystemCache(dir)
		return nil
	}
}

// WithRegistry sets a custom registry implementation.
func WithRegistry(r registry.Registry) Option {
	return func(cl *Client) error {
		cl.registry = r
		return nil
	}
}

// WithHTTPClient sets a custom HTTP client for the default registry.
func WithHTTPClient(client *http.Client) Option {
	return func(cl *Client) error {
		cl.registry = registry.NewTerraformRegistry(client)
		return nil
	}
}
