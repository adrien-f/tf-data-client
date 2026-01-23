package tfclient

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/adrien-f/tf-data-client/internal/tfplugin6"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-plugin"
	"github.com/zclconf/go-cty/cty/msgpack"
	"google.golang.org/grpc"
)

// protocolVersionRegex extracts version numbers from go-plugin's incompatibility error.
// Example: "incompatible API version with plugin. Plugin version: 5, Client versions: [6]"
var protocolVersionRegex = regexp.MustCompile(`Plugin version:\s*(\d+).*Client versions:\s*\[(\d+)\]`)

// errProtocolMismatch is an internal error type used to communicate protocol
// version mismatch details from launchProvider to the caller.
type errProtocolMismatch struct {
	pluginVersion int
	clientVersion int
}

func (e *errProtocolMismatch) Error() string {
	return fmt.Sprintf("protocol version mismatch: plugin v%d, client v%d", e.pluginVersion, e.clientVersion)
}

// handshake configuration matching Terraform/OpenTofu
var handshake = plugin.HandshakeConfig{
	ProtocolVersion:  4,
	MagicCookieKey:   "TF_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2",
}

// grpcProviderPlugin implements plugin.GRPCPlugin
type grpcProviderPlugin struct {
	plugin.Plugin
}

func (p *grpcProviderPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return tfplugin6.NewProviderClient(c), nil
}

func (p *grpcProviderPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	return nil // Client only
}

// DataSourceResult contains the result of reading a data source.
type DataSourceResult struct {
	State map[string]interface{}
}

// Provider is the interface for interacting with a Terraform provider.
type Provider interface {
	Configure(ctx context.Context, config map[string]interface{}) error
	ReadDataSource(ctx context.Context, typeName string, config map[string]interface{}) (*DataSourceResult, error)
	IsConfigured() bool
	ListDataSources() []string
	Close() error
}

// provider wraps a GRPC provider client.
type provider struct {
	// Public metadata
	Namespace string
	Name      string
	Version   string

	// Private fields
	pluginClient *plugin.Client
	grpcClient   tfplugin6.ProviderClient
	schema       *tfplugin6.GetProviderSchema_Response
	configured   bool
}

// launchProvider starts a provider binary and connects to it.
func launchProvider(execPath string, logger logr.Logger) (*provider, error) {
	config := &plugin.ClientConfig{
		HandshakeConfig:  handshake,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Managed:          true,
		Cmd:              exec.Command(execPath),
		AutoMTLS:         true,
		Logger:           newHclogAdapter(logger),
		VersionedPlugins: map[int]plugin.PluginSet{
			6: {"provider": &grpcProviderPlugin{}},
		},
	}

	client := plugin.NewClient(config)

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		// Check for protocol version mismatch
		if matches := protocolVersionRegex.FindStringSubmatch(err.Error()); matches != nil {
			pluginVer, _ := strconv.Atoi(matches[1])
			clientVer, _ := strconv.Atoi(matches[2])
			return nil, &errProtocolMismatch{
				pluginVersion: pluginVer,
				clientVersion: clientVer,
			}
		}
		return nil, fmt.Errorf("failed to get RPC client: %w", err)
	}

	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense provider: %w", err)
	}

	grpcClient, ok := raw.(tfplugin6.ProviderClient)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected provider type: %T", raw)
	}

	return &provider{
		pluginClient: client,
		grpcClient:   grpcClient,
	}, nil
}

// getSchema retrieves the provider schema.
func (p *provider) getSchema(ctx context.Context) error {
	resp, err := p.grpcClient.GetProviderSchema(ctx, &tfplugin6.GetProviderSchema_Request{})
	if err != nil {
		return fmt.Errorf("failed to get provider schema: %w", err)
	}

	if err := checkDiagnostics(resp.Diagnostics); err != nil {
		return fmt.Errorf("provider schema error: %w", err)
	}

	p.schema = resp
	return nil
}

// IsConfigured returns whether the provider has been configured.
func (p *provider) IsConfigured() bool {
	return p.configured
}

// Configure configures the provider with the given configuration.
func (p *provider) Configure(ctx context.Context, config map[string]interface{}) error {
	if p.schema == nil {
		return fmt.Errorf("schema not loaded")
	}

	providerSchema := p.schema.Provider
	if providerSchema == nil {
		return fmt.Errorf("provider schema not found")
	}

	schemaType, err := schemaBlockToType(providerSchema.Block)
	if err != nil {
		return fmt.Errorf("failed to convert provider schema to type: %w", err)
	}

	configValue, err := mapToCtyValue(config, schemaType)
	if err != nil {
		return fmt.Errorf("failed to convert config to cty value: %w", err)
	}

	configBytes, err := msgpack.Marshal(configValue, schemaType)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	resp, err := p.grpcClient.ConfigureProvider(ctx, &tfplugin6.ConfigureProvider_Request{
		TerraformVersion: "1.0.0",
		Config:           &tfplugin6.DynamicValue{Msgpack: configBytes},
	})
	if err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	if err := checkDiagnostics(resp.Diagnostics); err != nil {
		return fmt.Errorf("configure provider error: %w", err)
	}

	p.configured = true
	return nil
}

// ListDataSources returns the list of available data source types.
func (p *provider) ListDataSources() []string {
	if p.schema == nil {
		return nil
	}
	var names []string
	for name := range p.schema.DataSourceSchemas {
		names = append(names, name)
	}
	return names
}

// ReadDataSource reads a data source and returns the result.
func (p *provider) ReadDataSource(ctx context.Context, typeName string, config map[string]interface{}) (*DataSourceResult, error) {
	if p.schema == nil {
		return nil, fmt.Errorf("schema not loaded")
	}

	dataSourceSchema, ok := p.schema.DataSourceSchemas[typeName]
	if !ok {
		return nil, &ErrDataSourceNotFound{
			TypeName:  typeName,
			Namespace: p.Namespace,
			Name:      p.Name,
		}
	}

	schemaType, err := schemaBlockToType(dataSourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert data source schema to type: %w", err)
	}

	configValue, err := mapToCtyValue(config, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config to cty value: %w", err)
	}

	configBytes, err := msgpack.Marshal(configValue, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	resp, err := p.grpcClient.ReadDataSource(ctx, &tfplugin6.ReadDataSource_Request{
		TypeName: typeName,
		Config:   &tfplugin6.DynamicValue{Msgpack: configBytes},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read data source: %w", err)
	}

	if err := checkDiagnostics(resp.Diagnostics); err != nil {
		return nil, fmt.Errorf("read data source error: %w", err)
	}

	state, err := decodeDynamicValue(resp.State, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}

	stateMap, err := ctyValueToMap(state)
	if err != nil {
		return nil, fmt.Errorf("failed to convert state to map: %w", err)
	}

	return &DataSourceResult{State: stateMap}, nil
}

// Close shuts down the provider process.
func (p *provider) Close() error {
	if p.pluginClient != nil {
		p.pluginClient.Kill()
	}
	return nil
}

// checkDiagnostics checks for errors in diagnostics.
func checkDiagnostics(diags []*tfplugin6.Diagnostic) error {
	for _, diag := range diags {
		if diag.Severity == tfplugin6.Diagnostic_ERROR {
			return fmt.Errorf("%s: %s", diag.Summary, diag.Detail)
		}
	}
	return nil
}
