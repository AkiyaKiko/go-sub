package lib

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	urlpath "path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type Settings struct {
	Host           string          `json:"host"`
	Port           int             `json:"port"`
	Admin          string          `json:"admin"`
	Password       string          `json:"password"`
	TLS            *TLSConfig      `json:"tls,omitempty"`
	SingBox        *SingBoxOptions `json:"singbox,omitempty"`
	TrustedProxies []string        `json:"trusted_proxies"`
}

type Config struct {
	Settings *Settings `json:"settings"`
	Nodes    []string  `json:"nodes,omitempty"`
}

type bootstrapConfig struct {
	Settings *configSettings `json:"settings"`
	Nodes    json.RawMessage `json:"nodes,omitempty"`
}

type configSettings struct {
	Host           string          `json:"host"`
	Port           int             `json:"port"`
	Path           json.RawMessage `json:"path,omitempty"`
	Admin          string          `json:"admin"`
	Password       string          `json:"password"`
	TLS            json.RawMessage `json:"tls,omitempty"`
	SingBox        *SingBoxOptions `json:"singbox,omitempty"`
	TrustedProxies []string        `json:"trusted_proxies,omitempty"`
}

var (
	configFileMu   sync.RWMutex
	configFilePath string
)

func SetConfigPath(path string) {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	configFilePath = path
}

func LoadConfigFile() (*Config, error) {
	configFileMu.RLock()
	defer configFileMu.RUnlock()
	path := configFilePath
	if path == "" {
		return nil, fmt.Errorf("config path not set")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(f)
}

func UpdateSingBoxMode(mode string) error {
	if mode != "tun" && mode != "mixed" {
		return fmt.Errorf("singbox mode must be tun or mixed")
	}
	configFileMu.Lock()
	defer configFileMu.Unlock()
	path := configFilePath
	if path == "" {
		return fmt.Errorf("config path not set")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := ParseConfig(bytes.NewReader(content)); err != nil {
		return err
	}
	// Patch only this option, preserving ignored legacy fields and loopback TLS verbatim.
	var document map[string]json.RawMessage
	var settings map[string]json.RawMessage
	var options map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		return err
	}
	if err := json.Unmarshal(document["settings"], &settings); err != nil {
		return err
	}
	if raw := settings["singbox"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &options); err != nil {
			return err
		}
	}
	if options == nil {
		options = map[string]json.RawMessage{}
	}
	options["mode"], err = json.Marshal(mode)
	if err != nil {
		return err
	}
	settings["singbox"], err = json.Marshal(options)
	if err != nil {
		return err
	}
	document["settings"], err = json.Marshal(settings)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, encoded)
}

func writePrivateFile(path string, encoded []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gosub-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func ParseConfig(r io.Reader) (*Config, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return nil, fmt.Errorf("config error: cannot read configuration or size exceeds 1 MiB")
	}
	validation := json.NewDecoder(bytes.NewReader(raw))
	validation.UseNumber()
	if _, err := readTemplateJSON(validation, 0); err != nil {
		return nil, fmt.Errorf("config error: invalid JSON, duplicate keys or excessive nesting")
	}
	if _, err := validation.Token(); err != io.EOF {
		return nil, fmt.Errorf("config error: trailing JSON data")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var bootstrap bootstrapConfig
	if err := dec.Decode(&bootstrap); err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	settings, err := bootstrap.Settings.toSettings()
	if err != nil {
		return nil, fmt.Errorf("config error: invalid settings.tls: %w", err)
	}
	cfg := Config{Settings: settings}
	if err := NormalizeConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (settings *configSettings) toSettings() (*Settings, error) {
	if settings == nil {
		return nil, nil
	}
	normalized := &Settings{
		Host:           settings.Host,
		Port:           settings.Port,
		Admin:          settings.Admin,
		Password:       settings.Password,
		SingBox:        settings.SingBox,
		TrustedProxies: settings.TrustedProxies,
	}
	// The default loopback listener ignores TLS, including certificate validation.
	if settings.Host != "" && settings.Host != "127.0.0.1" && len(settings.TLS) > 0 {
		dec := json.NewDecoder(bytes.NewReader(settings.TLS))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&normalized.TLS); err != nil {
			return nil, err
		}
	}
	return normalized, nil
}

func NormalizeConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config error: nil config")
	}
	if err := NormalizeSettings(cfg.Settings); err != nil {
		return err
	}
	cfg.Nodes = NormalizeNodes(cfg.Nodes)
	return nil
}

func NormalizeSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf(`config error: "settings" field is required`)
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return fmt.Errorf(`config error: "settings.port" must be between 1 and 65535`)
	}
	for _, proxy := range settings.TrustedProxies {
		if net.ParseIP(proxy) == nil {
			if _, _, err := net.ParseCIDR(proxy); err != nil {
				return fmt.Errorf("config error: trusted_proxies requires IP addresses or CIDRs")
			}
		}
	}

	if settings.Host == "" {
		settings.Host = "127.0.0.1"
	}
	switch settings.Host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::", "::1":
	default:
		return fmt.Errorf(
			`config error: invalid "settings.host"=%q, allowed: localhost, 127.0.0.1, 0.0.0.0, ::, ::1`,
			settings.Host,
		)
	}

	if settings.Admin == "" || settings.Password == "" {
		return fmt.Errorf(`config error: "settings.admin" and "settings.password" are required`)
	}
	if settings.Host == "127.0.0.1" || settings.TLS == nil {
		settings.TLS = &TLSConfig{Enabled: false}
	}
	if !settings.TLS.Enabled {
		settings.TLS.CertFile = ""
		settings.TLS.KeyFile = ""
		return nil
	}
	if settings.TLS.CertFile == "" || settings.TLS.KeyFile == "" {
		return fmt.Errorf(`config error: when TLS is enabled, "settings.tls.certFile" and "settings.tls.keyFile" are required`)
	}
	return nil
}

func NormalizeNodes(nodes []string) []string {
	cleaned := make([]string, 0, len(nodes))
	for _, node := range nodes {
		node = strings.TrimSpace(node)
		if node != "" {
			cleaned = append(cleaned, node)
		}
	}
	return cleaned
}

func ValidateSubscriptionPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("subscription path is required")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("subscription path must start with /")
	}
	if !utf8.ValidString(path) || strings.ContainsFunc(path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("subscription path must not contain whitespace")
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("subscription path must not contain query or fragment")
	}
	if len(path) > 128 {
		return fmt.Errorf("subscription path is too long")
	}
	if strings.ContainsAny(path, "\\%") || strings.Contains(path, "//") || urlpath.Clean(path) != path {
		return fmt.Errorf("subscription path must be a canonical URL path")
	}
	if isReservedPublicPath(path) {
		return fmt.Errorf("subscription path is reserved")
	}
	return nil
}

func isReservedPublicPath(path string) bool {
	if path == "/" {
		return true
	}
	for _, prefix := range []string{
		"/api",
		"/assets",
		"/login",
		"/logout",
		"/nodes",
		"/route-rules",
		"/subscription-path",
		"/singbox-settings",
		"/singbox-template",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func CloneSettings(settings *Settings) *Settings {
	if settings == nil {
		return nil
	}
	cloned := *settings
	if settings.TrustedProxies != nil {
		cloned.TrustedProxies = append([]string{}, settings.TrustedProxies...)
	}
	if settings.SingBox != nil {
		options := *settings.SingBox
		cloned.SingBox = &options
	}
	if settings.TLS != nil {
		tls := *settings.TLS
		cloned.TLS = &tls
	}
	return &cloned
}

func EncodeConfig(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("EncodeConfig: nil config")
	}
	settings := CloneSettings(cfg.Settings)
	if err := NormalizeSettings(settings); err != nil {
		return nil, err
	}
	return json.MarshalIndent(struct {
		Settings *Settings `json:"settings"`
	}{Settings: settings}, "", "  ")
}

func Authenticate(username, password string) bool {
	settings, err := GetSettings()
	if err != nil {
		return false
	}
	userMatches := secureCompare(username, settings.Admin)
	passwordMatches := secureCompare(password, settings.Password)
	return userMatches && passwordMatches
}

func CredentialsVersion(settings *Settings) [32]byte {
	encoded, _ := json.Marshal([2]string{settings.Admin, settings.Password})
	return sha256.Sum256(encoded)
}

func secureCompare(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
