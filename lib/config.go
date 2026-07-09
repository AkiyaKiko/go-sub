package lib

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type Settings struct {
	Host     string     `json:"host"`
	Port     int        `json:"port"`
	Admin    string     `json:"admin"`
	Password string     `json:"password"`
	TLS      *TLSConfig `json:"tls,omitempty"`
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
	Host     string          `json:"host"`
	Port     int             `json:"port"`
	Path     json.RawMessage `json:"path,omitempty"`
	Admin    string          `json:"admin"`
	Password string          `json:"password"`
	TLS      *TLSConfig      `json:"tls,omitempty"`
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

func getConfigPath() string {
	configFileMu.RLock()
	defer configFileMu.RUnlock()
	return configFilePath
}

func LoadConfigFile() (*Config, error) {
	path := getConfigPath()
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

func ParseConfig(r io.Reader) (*Config, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var bootstrap bootstrapConfig
	if err := dec.Decode(&bootstrap); err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	cfg := Config{Settings: bootstrap.Settings.toSettings()}
	if err := NormalizeConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (settings *configSettings) toSettings() *Settings {
	if settings == nil {
		return nil
	}
	return &Settings{
		Host:     settings.Host,
		Port:     settings.Port,
		Admin:    settings.Admin,
		Password: settings.Password,
		TLS:      settings.TLS,
	}
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
	if settings.Port == 0 {
		return fmt.Errorf(`config error: "settings.port" is required`)
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

	if settings.TLS == nil {
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
	if strings.ContainsAny(path, " \t\r\n") {
		return fmt.Errorf("subscription path must not contain whitespace")
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("subscription path must not contain query or fragment")
	}
	if len(path) > 128 {
		return fmt.Errorf("subscription path is too long")
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
	return secureCompare(username, settings.Admin) && secureCompare(password, settings.Password)
}

func secureCompare(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
