# OpenTofu Data Client

A Go library and CLI for directly communicating with Terraform/OpenTofu providers over gRPC without spawning the OpenTofu process. Download providers from the registry, launch them as subprocesses, and call their data sources programmatically.

> ![NOTE]
> While OpenTofu is mentioned in the codebase, the current Hashicorp Terraform registry is used
> The plan is to migrate at a later date to the new registry when ready


## Installation

### Library

```bash
go get github.com/adrien-f/opentofu-data-client
```

### CLI

```bash
go install github.com/adrien-f/opentofu-data-client/cmd/opentofu-data-client@latest
```

## Library Usage

### Basic Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    otfclient "github.com/adrien-f/opentofu-data-client"
)

func main() {
    ctx := context.Background()

    // Create a client with default settings
    client, err := otfclient.New()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Create a provider (downloads if not cached, launches, fetches schema)
    provider, err := client.CreateProvider(ctx, otfclient.ProviderConfig{
        Namespace: "hashicorp",
        Name:      "aws",
        // Version: "5.0.0",  // Optional: omit for latest
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Provider: %s/%s@%s\n", provider.Namespace, provider.Name, provider.Version)

    // Configure the provider
    if err := provider.Configure(ctx, map[string]interface{}{
        "region": "us-west-2",
    }); err != nil {
        log.Fatal(err)
    }

    // List available data sources
    fmt.Println("Data sources:", provider.ListDataSources())

    // Read a data source
    result, err := provider.ReadDataSource(ctx, "aws_caller_identity", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Account ID: %s\n", result.State["account_id"])
}
```

### Custom Cache Directory

```go
client, err := otfclient.New(
    otfclient.WithCacheDir("/var/cache/providers"),
)
```

### Custom HTTP Client

```go
httpClient := &http.Client{
    Timeout: 30 * time.Second,
}

client, err := otfclient.New(
    otfclient.WithHTTPClient(httpClient),
)
```

### Kubernetes Provider Example

```go
provider, err := client.CreateProvider(ctx, otfclient.ProviderConfig{
    Namespace: "hashicorp",
    Name:      "kubernetes",
})
if err != nil {
    log.Fatal(err)
}

// Configure with kubeconfig
err = provider.Configure(ctx, map[string]interface{}{
    "config_path": "~/.kube/config",
})
if err != nil {
    log.Fatal(err)
}

// Read all namespaces
result, err := provider.ReadDataSource(ctx, "kubernetes_all_namespaces", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Namespaces: %v\n", result.State["namespaces"])
```

### AWS Provider Example

```go
provider, err := client.CreateProvider(ctx, otfclient.ProviderConfig{
    Namespace: "hashicorp",
    Name:      "aws",
    Version:   "5.0.0",
})
if err != nil {
    log.Fatal(err)
}

err = provider.Configure(ctx, map[string]interface{}{
    "region": "us-west-2",
})
if err != nil {
    log.Fatal(err)
}

// Get current AWS caller identity
result, err := provider.ReadDataSource(ctx, "aws_caller_identity", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Account ID: %s\n", result.State["account_id"])
```

## CLI Usage

### List Data Sources

```bash
opentofu-data-client \
  --provider hashicorp/kubernetes \
  --list-data-sources
```

### Read a Data Source

```bash
opentofu-data-client \
  --provider hashicorp/kubernetes \
  --config '{"config_path": "~/.kube/config"}' \
  --data-source kubernetes_all_namespaces
```

### Pin Provider Version

```bash
opentofu-data-client \
  --provider hashicorp/aws \
  --version 5.0.0 \
  --config '{"region": "us-west-2"}' \
  --data-source aws_caller_identity
```

### Output to File

```bash
opentofu-data-client \
  --provider hashicorp/aws \
  --config '{"region": "us-west-2"}' \
  --data-source aws_caller_identity \
  --output result.json
```

### Custom Cache Directory

```bash
opentofu-data-client \
  --provider hashicorp/aws \
  --cache-dir /tmp/providers \
  --list-data-sources
```

## Package Structure

```
github.com/adrien-f/opentofu-data-client/
├── client.go              # Client type - main entry point
├── options.go             # Functional options
├── errors.go              # Custom error types
├── provider.go            # Provider type
├── schema.go              # Schema conversion helpers
├── cache/
│   ├── cache.go           # Cache interface
│   ├── filesystem.go      # Filesystem cache implementation
│   └── types.go           # ProviderIdentifier
├── registry/
│   ├── registry.go        # Registry interface + Terraform implementation
│   └── types.go           # VersionInfo, DownloadInfo
└── cmd/opentofu-data-client/
    └── main.go            # CLI
```

## Interfaces

### Cache Interface

Implement custom caching (e.g., S3, GCS) by implementing the `cache.Cache` interface:

```go
type Cache interface {
    Get(ctx context.Context, id ProviderIdentifier) (executablePath string, err error)
    Put(ctx context.Context, id ProviderIdentifier, archivePath string) (executablePath string, err error)
    Has(ctx context.Context, id ProviderIdentifier) (bool, error)
}
```

### Registry Interface

Implement custom registries by implementing the `registry.Registry` interface:

```go
type Registry interface {
    GetVersions(ctx context.Context, namespace, name string) ([]VersionInfo, error)
    GetLatestVersion(ctx context.Context, namespace, name string) (string, error)
    GetDownloadInfo(ctx context.Context, namespace, name, version, os, arch string) (*DownloadInfo, error)
    DownloadToPath(ctx context.Context, info *DownloadInfo, destPath string) error
}
```

## Error Handling

The library provides typed errors for common failure scenarios:

```go
provider, err := client.CreateProvider(ctx, cfg)
if err != nil {
    var notFound *otfclient.ErrProviderNotFound
    var versionNotFound *otfclient.ErrVersionNotFound
    var downloadFailed *otfclient.ErrDownloadFailed
    var launchFailed *otfclient.ErrLaunchFailed
    var protocolErr *otfclient.ErrProtocolUnsupported

    switch {
    case errors.As(err, &notFound):
        fmt.Printf("Provider %s/%s not found\n", notFound.Namespace, notFound.Name)
    case errors.As(err, &versionNotFound):
        fmt.Printf("Version %s not found\n", versionNotFound.Version)
    case errors.As(err, &downloadFailed):
        fmt.Printf("Download failed: %v\n", downloadFailed.Unwrap())
    case errors.As(err, &launchFailed):
        fmt.Printf("Launch failed: %v\n", launchFailed.Unwrap())
    case errors.As(err, &protocolErr):
        fmt.Printf("Provider %s/%s@%s uses protocol v%d (only v%d supported)\n",
            protocolErr.Namespace, protocolErr.Name, protocolErr.Version,
            protocolErr.ProviderVersion, protocolErr.ClientVersion)
    default:
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Limitations

- No lock file or checksum verification
- No GPG signature verification
- Data sources only (no resource management)

### Protocol v6 Only

This client only supports providers that implement **Protocol v6** (tfplugin6). Some older providers still use Protocol v5 and are not compatible.

**Known v5-only providers** (incompatible):
- `hashicorp/random` (as of v3.8.0)

**Compatible v6 providers** include:
- `hashicorp/aws`
- `hashicorp/google`
- `hashicorp/kubernetes`
- `hashicorp/azurerm`
- Most actively maintained providers

If you encounter an error like:
```
provider hashicorp/random@3.8.0 uses plugin protocol v5, but this client only supports protocol v6
```
This means the provider hasn't been updated to support Protocol v6. Check if a newer version exists or try an alternative provider

## License

MIT
