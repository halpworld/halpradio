package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

const (
	DefaultRegistryURL = "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/registry.json"
)

// Size limits for registry downloads.
const (
	maxRegistryBytes     = 2 << 20  // registry.json
	maxWasmDownloadBytes = 20 << 20 // plugin.wasm
)

// RegistryClient interacts with the remote official plugin registry.
type RegistryClient struct {
	httpClient  *http.Client
	registryURL string
	// trustedKeys is the publisher key set used for signature checks
	// (trustedPublisherKeys; replaceable in tests).
	trustedKeys []string
	// allowInsecureForTests permits http:// URLs. Only tests set it; it is
	// never set from configuration.
	allowInsecureForTests bool
}

// newRegistryHTTPClient returns a client that only talks to public
// addresses: the dialer resolves the host itself and refuses loopback,
// private, link-local and metadata addresses, so a hostile registry entry
// cannot make the installer probe the user's machine or LAN. Proxy
// environment variables are ignored because a proxy would move that check
// off the real destination.
func newRegistryHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid network address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve host %q: %w", host, err)
			}
			for _, ip := range ips {
				if !IsPrivateOrLoopbackIP(ip) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				}
			}
			return nil, fmt.Errorf("refusing to download from %q: it resolves only to local or private addresses", host)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing redirect to non-https URL %s", req.URL.Redacted())
			}
			return nil
		},
	}
}

// NewRegistryClient creates a new registry client instance.
func NewRegistryClient(registryURL string) *RegistryClient {
	if registryURL == "" {
		registryURL = DefaultRegistryURL
	}
	return &RegistryClient{
		httpClient:  newRegistryHTTPClient(),
		registryURL: registryURL,
		trustedKeys: trustedPublisherKeys,
	}
}

func (c *RegistryClient) checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}
	if u.Scheme == "https" || (c.allowInsecureForTests && u.Scheme == "http") {
		return u, nil
	}
	return nil, fmt.Errorf("refusing non-https URL %q", u.Redacted())
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

// FetchRegistry downloads and decodes the latest registry index. Entries
// that fail ValidateRegistryPlugin, and repeated IDs, are dropped and
// counted in Skipped; nothing from a dropped entry is ever displayed.
func (c *RegistryClient) FetchRegistry(ctx context.Context) (*RegistryIndex, error) {
	if _, err := c.checkURL(c.registryURL); err != nil {
		return nil, fmt.Errorf("plugin registry URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.registryURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "halpradio/plugin-manager")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch plugin registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry server returned HTTP %d", resp.StatusCode)
	}

	data, err := readBounded(resp.Body, maxRegistryBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry response: %w", err)
	}

	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse registry JSON: %w", err)
	}
	index.Version = util.SanitizeDisplay(index.Version, 32)
	index.UpdatedAt = util.SanitizeDisplay(index.UpdatedAt, 64)

	seen := make(map[string]bool, len(index.Plugins))
	valid := make([]RegistryPlugin, 0, len(index.Plugins))
	for _, p := range index.Plugins {
		if seen[p.ID] || ValidateRegistryPlugin(p) != nil {
			index.Skipped++
			continue
		}
		seen[p.ID] = true
		valid = append(valid, p)
	}
	index.Plugins = valid
	return &index, nil
}

func (c *RegistryClient) resolveDownloadURL(raw string) (string, error) {
	base, err := url.Parse(c.registryURL)
	if err != nil {
		return "", fmt.Errorf("invalid registry URL: %w", err)
	}
	rel, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid download URL %q", raw)
	}
	u, err := c.checkURL(base.ResolveReference(rel).String())
	if err != nil {
		return "", fmt.Errorf("download URL: %w", err)
	}
	return u.String(), nil
}

// FetchVerified validates a registry entry, checks its publisher signature
// policy, downloads the binary (bounded) and verifies its SHA-256. Nothing
// is written to disk. It returns the binary and the manifest to install.
func (c *RegistryClient) FetchVerified(ctx context.Context, plugin RegistryPlugin) ([]byte, Manifest, error) {
	if plugin.ID == "" || !validIDRegex.MatchString(plugin.ID) {
		return nil, Manifest{}, fmt.Errorf("invalid plugin ID %q", plugin.ID)
	}
	checksum := strings.TrimSpace(strings.ToLower(plugin.Checksum))
	if checksum == "" {
		return nil, Manifest{}, fmt.Errorf("security policy violation: plugin %q has no SHA-256 checksum in registry", plugin.ID)
	}
	if len(checksum) != 64 {
		return nil, Manifest{}, fmt.Errorf("invalid SHA-256 checksum format for plugin %q (expected 64 hex chars)", plugin.ID)
	}
	if err := ValidateRegistryPlugin(plugin); err != nil {
		return nil, Manifest{}, fmt.Errorf("manifest validation failed: %w", err)
	}
	manifest := plugin.toManifest()
	if err := manifest.Validate(); err != nil {
		return nil, Manifest{}, fmt.Errorf("manifest validation failed: %w", err)
	}
	if err := verifyPublisher(plugin, c.trustedKeys); err != nil {
		return nil, Manifest{}, err
	}

	downloadURL, err := c.resolveDownloadURL(plugin.DownloadURL)
	if err != nil {
		return nil, Manifest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, Manifest{}, fmt.Errorf("failed to construct wasm download request: %w", err)
	}
	req.Header.Set("User-Agent", "halpradio/plugin-installer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, Manifest{}, fmt.Errorf("failed to download wasm binary: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, Manifest{}, fmt.Errorf("wasm download returned HTTP %d", resp.StatusCode)
	}
	wasmBytes, err := readBounded(resp.Body, maxWasmDownloadBytes)
	if err != nil {
		return nil, Manifest{}, fmt.Errorf("failed to read wasm binary: %w", err)
	}
	if err := VerifyChecksum(wasmBytes, checksum); err != nil {
		return nil, Manifest{}, fmt.Errorf("plugin %s failed security checksum verification: %w", plugin.ID, err)
	}
	return wasmBytes, manifest, nil
}

// DownloadAndInstall downloads, verifies and atomically installs a plugin
// into <pluginsBaseDir>/<id>/. Nothing is written unless every check passes.
func (c *RegistryClient) DownloadAndInstall(ctx context.Context, plugin RegistryPlugin, pluginsBaseDir string) error {
	wasm, manifest, err := c.FetchVerified(ctx, plugin)
	if err != nil {
		return err
	}
	return writePluginDir(pluginsBaseDir, manifest, wasm)
}

func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writePluginDir stages manifest.yaml and plugin.wasm in a temporary
// directory next to the target and renames it into place, so a failed or
// interrupted install never leaves a new binary beside an old manifest.
func writePluginDir(pluginsBaseDir string, manifest Manifest, wasm []byte) error {
	if !validIDRegex.MatchString(manifest.ID) {
		return fmt.Errorf("invalid plugin ID %q", manifest.ID)
	}
	manifest.WasmFile = "plugin.wasm"
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}
	manifestBytes, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}
	if err := os.MkdirAll(pluginsBaseDir, 0700); err != nil {
		return fmt.Errorf("failed to create plugins directory: %w", err)
	}
	tmp, err := os.MkdirTemp(pluginsBaseDir, ".tmp-"+manifest.ID+"-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := writeFileSync(filepath.Join(tmp, "plugin.wasm"), wasm); err != nil {
		return fmt.Errorf("failed to save wasm binary: %w", err)
	}
	if err := writeFileSync(filepath.Join(tmp, "manifest.yaml"), manifestBytes); err != nil {
		return fmt.Errorf("failed to save manifest.yaml: %w", err)
	}

	final := filepath.Join(pluginsBaseDir, manifest.ID)
	var old string
	if _, err := os.Lstat(final); err == nil {
		old = filepath.Join(pluginsBaseDir, fmt.Sprintf(".old-%s-%d", manifest.ID, time.Now().UnixNano()))
		if err := os.Rename(final, old); err != nil {
			return fmt.Errorf("failed to replace existing plugin: %w", err)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		if old != "" {
			_ = os.Rename(old, final)
		}
		return fmt.Errorf("failed to move plugin into place: %w", err)
	}
	cleanup = false
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}
