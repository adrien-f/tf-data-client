package registry

// VersionInfo contains information about a provider version.
type VersionInfo struct {
	Version   string
	Protocols []string
}

// DownloadInfo contains information for downloading a provider.
type DownloadInfo struct {
	OS          string
	Arch        string
	Filename    string
	DownloadURL string
	SHA256Sum   string
}
