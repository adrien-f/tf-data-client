package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Registry defines the interface for provider registries.
type Registry interface {
	// GetVersions returns all available versions for a provider.
	GetVersions(ctx context.Context, namespace, name string) ([]VersionInfo, error)

	// GetLatestVersion returns the latest version for a provider.
	GetLatestVersion(ctx context.Context, namespace, name string) (string, error)

	// GetDownloadInfo returns download information for a specific provider version.
	GetDownloadInfo(ctx context.Context, namespace, name, version, os, arch string) (*DownloadInfo, error)

	// DownloadToPath downloads the provider archive to a local path.
	DownloadToPath(ctx context.Context, info *DownloadInfo, destPath string) error
}

const terraformRegistryBaseURL = "https://registry.terraform.io/v1/providers"

// TerraformRegistry implements Registry for the Terraform/OpenTofu registry.
type TerraformRegistry struct {
	client  *http.Client
	baseURL string
}

// NewTerraformRegistry creates a new TerraformRegistry with the given HTTP client.
// If client is nil, http.DefaultClient is used.
func NewTerraformRegistry(client *http.Client) *TerraformRegistry {
	if client == nil {
		client = http.DefaultClient
	}
	return &TerraformRegistry{
		client:  client,
		baseURL: terraformRegistryBaseURL,
	}
}

type versionsResponse struct {
	Versions []struct {
		Version   string   `json:"version"`
		Protocols []string `json:"protocols"`
	} `json:"versions"`
}

type downloadResponse struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
	SHA256Sum   string `json:"shasum"`
}

// GetVersions returns all available versions for a provider.
func (r *TerraformRegistry) GetVersions(ctx context.Context, namespace, name string) ([]VersionInfo, error) {
	url := fmt.Sprintf("%s/%s/%s/versions", r.baseURL, namespace, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("provider %s/%s not found", namespace, name)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var versions versionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, fmt.Errorf("failed to decode versions response: %w", err)
	}

	result := make([]VersionInfo, len(versions.Versions))
	for i, v := range versions.Versions {
		result[i] = VersionInfo{
			Version:   v.Version,
			Protocols: v.Protocols,
		}
	}

	return result, nil
}

// GetLatestVersion returns the latest version for a provider.
func (r *TerraformRegistry) GetLatestVersion(ctx context.Context, namespace, name string) (string, error) {
	versions, err := r.GetVersions(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found for provider %s/%s", namespace, name)
	}

	// Sort versions semantically to find the latest
	sort.Slice(versions, func(i, j int) bool {
		mi, ni, pi := semverParts(versions[i].Version)
		mj, nj, pj := semverParts(versions[j].Version)
		if mi != mj {
			return mi < mj
		}
		if ni != nj {
			return ni < nj
		}
		return pi < pj
	})

	// Return the last version (highest semver)
	return versions[len(versions)-1].Version, nil
}

// GetDownloadInfo returns download information for a specific provider version.
func (r *TerraformRegistry) GetDownloadInfo(ctx context.Context, namespace, name, version, goos, goarch string) (*DownloadInfo, error) {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	url := fmt.Sprintf("%s/%s/%s/%s/download/%s/%s", r.baseURL, namespace, name, version, goos, goarch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch download info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("version %s not found for provider %s/%s", version, namespace, name)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for download info", resp.StatusCode)
	}

	var dl downloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&dl); err != nil {
		return nil, fmt.Errorf("failed to decode download response: %w", err)
	}

	return &DownloadInfo{
		OS:          dl.OS,
		Arch:        dl.Arch,
		Filename:    dl.Filename,
		DownloadURL: dl.DownloadURL,
		SHA256Sum:   dl.SHA256Sum,
	}, nil
}

// DownloadToPath downloads the provider archive to a local path.
func (r *TerraformRegistry) DownloadToPath(ctx context.Context, info *DownloadInfo, destPath string) error {
	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// semverParts parses a semantic version string into major, minor, patch.
func semverParts(v string) (int, int, int) {
	// Remove leading 'v' if present
	v = strings.TrimPrefix(v, "v")
	// Remove any prerelease suffix (e.g., -beta, -rc1)
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	var major, minor, patch int
	if len(parts) >= 1 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return major, minor, patch
}
