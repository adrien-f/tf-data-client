package cache

import "context"

// Cache defines the interface for provider binary caching.
type Cache interface {
	// Get retrieves the executable path for a cached provider.
	// Returns empty string and nil error if the provider is not cached.
	Get(ctx context.Context, id ProviderIdentifier) (executablePath string, err error)

	// Put stores a provider archive and returns the path to the extracted executable.
	// archivePath is the path to the downloaded zip file.
	Put(ctx context.Context, id ProviderIdentifier, archivePath string) (executablePath string, err error)

	// Has checks if a provider is cached.
	Has(ctx context.Context, id ProviderIdentifier) (bool, error)
}
