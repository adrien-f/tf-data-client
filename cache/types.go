package cache

// ProviderIdentifier uniquely identifies a provider binary.
type ProviderIdentifier struct {
	Namespace string
	Name      string
	Version   string
	OS        string
	Arch      string
}
