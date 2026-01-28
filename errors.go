package tfclient

import "fmt"

// ErrProviderNotFound is returned when a provider cannot be found in the registry
// (e.g. resolving latest version or when the provider does not exist).
type ErrProviderNotFound struct {
	Namespace string
	Name      string
	Err       error
}

func (e *ErrProviderNotFound) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("provider not found: %s/%s: %v", e.Namespace, e.Name, e.Err)
	}
	return fmt.Sprintf("provider not found: %s/%s", e.Namespace, e.Name)
}

func (e *ErrProviderNotFound) Unwrap() error {
	return e.Err
}

// ErrVersionNotFound is returned when a specific version of a provider cannot be found.
type ErrVersionNotFound struct {
	Namespace string
	Name      string
	Version   string
}

func (e *ErrVersionNotFound) Error() string {
	return fmt.Sprintf("version %s not found for provider %s/%s", e.Version, e.Namespace, e.Name)
}

// ErrProviderNotConfigured is returned when attempting to use a provider that hasn't been configured.
type ErrProviderNotConfigured struct {
	Namespace string
	Name      string
}

func (e *ErrProviderNotConfigured) Error() string {
	return fmt.Sprintf("provider not configured: %s/%s", e.Namespace, e.Name)
}

// ErrDataSourceNotFound is returned when a data source type doesn't exist in the provider schema.
type ErrDataSourceNotFound struct {
	TypeName  string
	Namespace string
	Name      string
}

func (e *ErrDataSourceNotFound) Error() string {
	return fmt.Sprintf("data source %q not found in provider %s/%s", e.TypeName, e.Namespace, e.Name)
}

// ErrDownloadFailed is returned when provider download fails.
type ErrDownloadFailed struct {
	Namespace string
	Name      string
	Version   string
	Err       error
}

func (e *ErrDownloadFailed) Error() string {
	return fmt.Sprintf("failed to download provider %s/%s@%s: %v", e.Namespace, e.Name, e.Version, e.Err)
}

func (e *ErrDownloadFailed) Unwrap() error {
	return e.Err
}

// ErrLaunchFailed is returned when launching a provider process fails.
type ErrLaunchFailed struct {
	Namespace string
	Name      string
	Version   string
	Err       error
}

func (e *ErrLaunchFailed) Error() string {
	return fmt.Sprintf("failed to launch provider %s/%s@%s: %v", e.Namespace, e.Name, e.Version, e.Err)
}

func (e *ErrLaunchFailed) Unwrap() error {
	return e.Err
}

// ErrSchemaFailed is returned when fetching provider schema fails.
type ErrSchemaFailed struct {
	Namespace string
	Name      string
	Err       error
}

func (e *ErrSchemaFailed) Error() string {
	return fmt.Sprintf("failed to get schema for provider %s/%s: %v", e.Namespace, e.Name, e.Err)
}

func (e *ErrSchemaFailed) Unwrap() error {
	return e.Err
}

// ErrConfigureFailed is returned when configuring a provider fails.
type ErrConfigureFailed struct {
	Namespace string
	Name      string
	Err       error
}

func (e *ErrConfigureFailed) Error() string {
	return fmt.Sprintf("failed to configure provider %s/%s: %v", e.Namespace, e.Name, e.Err)
}

func (e *ErrConfigureFailed) Unwrap() error {
	return e.Err
}

// ErrProtocolUnsupported is returned when a provider uses an unsupported plugin protocol version.
type ErrProtocolUnsupported struct {
	Namespace       string
	Name            string
	Version         string
	ProviderVersion int
	ClientVersion   int
}

func (e *ErrProtocolUnsupported) Error() string {
	return fmt.Sprintf(
		"provider %s/%s@%s uses plugin protocol v%d, but this client only supports protocol v%d.\n"+
			"This is a limitation of older providers. Some providers (like hashicorp/random) still use v5.\n"+
			"Try using a provider that supports protocol v6, or check if a newer version of this provider exists",
		e.Namespace, e.Name, e.Version, e.ProviderVersion, e.ClientVersion)
}
