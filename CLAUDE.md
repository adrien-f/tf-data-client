# TF Data Client - Design Notes

## Overview

This is a proof-of-concept Go client that directly communicates with Terraform/OpenTofu providers over GRPC without spawning the Terraform/OpenTofu process. It downloads providers from the registry, launches them as subprocesses, and calls their data sources.

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────────┐
│   main.go       │────▶│   registry.go    │────▶│ Terraform Registry  │
│ (orchestration) │     │ (download)       │     │ registry.terraform.io│
└────────┬────────┘     └──────────────────┘     └─────────────────────┘
         │
         │ Launch & GRPC
         ▼
┌─────────────────┐     ┌──────────────────┐
│   provider.go   │────▶│ Provider Binary  │
│ (GRPC client)   │     │ (subprocess)     │
└─────────────────┘     └──────────────────┘
```

## Key Components

### 1. Registry Client (`registry.go`)

Downloads providers from the Terraform registry:

- **API Endpoints**:
  - `GET /v1/providers/{namespace}/{name}/versions` - List versions
  - `GET /v1/providers/{namespace}/{name}/{version}/download/{os}/{arch}` - Get download URL

- **Version Selection**: Parses semantic versions to find the latest

- **Caching**: Providers cached in `~/.tf-data-client/providers/{namespace}/{name}/{version}/`

### 2. Provider Launcher (`provider.go`)

Communicates with providers using HashiCorp's go-plugin:

- **Handshake Config** (from OpenTofu `internal/plugin6/serve.go`):
  ```go
  ProtocolVersion:  4
  MagicCookieKey:   "TF_PLUGIN_MAGIC_COOKIE"
  MagicCookieValue: "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2"
  ```

- **Protocol**: Uses tfplugin6 (Protocol v6) for modern providers

- **GRPC Calls**:
  1. `GetProviderSchema()` - Get provider and data source schemas
  2. `ConfigureProvider()` - Initialize provider with config
  3. `ReadDataSource()` - Read a data source

### 3. Value Encoding

- **Schema Types**: JSON-encoded cty types in proto schema
- **Values**: msgpack-encoded using `go-cty/cty/msgpack`
- **Conversion**: `map[string]interface{}` ↔ `cty.Value` via JSON intermediary

## Protocol Details

The provider plugin protocol is defined in `internal/tfplugin6/tfplugin6.proto`:

```protobuf
service Provider {
  rpc GetProviderSchema(GetProviderSchema.Request) returns (GetProviderSchema.Response);
  rpc ConfigureProvider(ConfigureProvider.Request) returns (ConfigureProvider.Response);
  rpc ReadDataSource(ReadDataSource.Request) returns (ReadDataSource.Response);
  // ... other RPCs
}

message DynamicValue {
  bytes msgpack = 1;  // Primary encoding
  bytes json = 2;     // Fallback
}
```

## Usage

```bash
# List available data sources
./tf-data-client --provider hashicorp/kubernetes --list-data-sources

# Read a data source
./tf-data-client \
  --provider hashicorp/kubernetes \
  --config '{"host": "https://...", "insecure": true, ...}' \
  --data-source kubernetes_all_namespaces \
  --output result.json
```

## Dependencies

- `github.com/hashicorp/go-plugin` - Plugin framework with GRPC support
- `github.com/zclconf/go-cty` - Type system and msgpack encoding
- `google.golang.org/grpc` - GRPC client
- `google.golang.org/protobuf` - Protobuf runtime

## Key Files Reference (OpenTofu)

| Purpose | Path |
|---------|------|
| Handshake config | `internal/plugin6/serve.go:28-36` |
| GRPC client | `internal/plugin6/grpc_provider.go` |
| Proto definitions | `internal/tfplugin6/tfplugin6.proto` |
| Registry client | `internal/getproviders/registry_client.go` |

## Limitations (First Iteration)

- No lock file or checksum verification
- No support for provider caching across multiple runs (version pinning)
- Schema type conversion is simplified (may not handle all nested types)
- Debug logging from go-plugin is verbose (goes to stderr)

## Future Improvements

- Add proper semantic version parsing library
- Implement provider lock file
- Add checksum/GPG signature verification
- Support for resources (not just data sources)
- Better error messages and diagnostics formatting
