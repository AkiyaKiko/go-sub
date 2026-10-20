package lib

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestUpdateSingBoxModePreservesConfigAndPersists(t *testing.T) {
	configFileMu.RLock()
	previousPath := configFilePath
	configFileMu.RUnlock()
	t.Cleanup(func() { SetConfigPath(previousPath) })
	for _, rawOptions := range []string{"", `,"singbox":null`, `,"singbox":{"mode":"mixed","mixed_port":2081,"allow_partial":true,"test_interval":"20m"}`} {
		t.Run(rawOptions, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "config.json")
			original := []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password","path":"/legacy","tls":{"enabled":"ignored","unknown":true}` + rawOptions + `},"nodes":["legacy-node"]}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			SetConfigPath(path)
			parentBefore, err := os.Stat(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"tun", "mixed"} {
				if err := UpdateSingBoxMode(mode); err != nil {
					t.Fatal(err)
				}
				cfg, err := LoadConfigFile()
				if err != nil || cfg.Settings.SingBox == nil || cfg.Settings.SingBox.Mode != mode {
					t.Fatalf("saved mode did not survive reload: %v", err)
				}
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var before, after map[string]any
				if err := json.Unmarshal(original, &before); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(content, &after); err != nil {
					t.Fatal(err)
				}
				beforeSettings, afterSettings := before["settings"].(map[string]any), after["settings"].(map[string]any)
				beforeOptions, _ := beforeSettings["singbox"].(map[string]any)
				if beforeOptions == nil {
					beforeOptions = map[string]any{}
				}
				beforeOptions["mode"] = mode
				beforeSettings["singbox"] = beforeOptions
				if !reflect.DeepEqual(before, after) || afterSettings["singbox"].(map[string]any)["mode"] != mode {
					t.Fatal("mode save changed unrelated configuration")
				}
			}
			content, _ := os.ReadFile(path)
			if err := UpdateSingBoxMode("invalid"); err == nil {
				t.Fatal("invalid mode accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(content, after) {
				t.Fatal("invalid mode changed config")
			}
			parentAfter, _ := os.Stat(directory)
			if parentBefore.Mode() != parentAfter.Mode() {
				t.Fatal("config save changed parent directory permissions")
			}
			files, _ := filepath.Glob(filepath.Join(directory, ".gosub-config-*"))
			if len(files) > 0 {
				t.Fatal("temporary config files were retained")
			}
		})
	}
}

func TestConcurrentSingBoxModeSaveAndConfigReads(t *testing.T) {
	configFileMu.RLock()
	previousPath := configFilePath
	configFileMu.RUnlock()
	t.Cleanup(func() { SetConfigPath(previousPath) })
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test","password":"test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	SetConfigPath(path)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			for j := 0; j < 8; j++ {
				if index%2 == 0 {
					if err := UpdateSingBoxMode([]string{"tun", "mixed"}[j%2]); err != nil {
						t.Error(err)
					}
				} else if _, err := LoadConfigFile(); err != nil {
					t.Error("config reader observed an incomplete file")
				}
			}
		}(i)
	}
	workers.Wait()
}

func TestParseConfigTLSByHost(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		tls         string
		wantEnabled bool
		wantError   bool
	}{
		{
			name: "loopback ignores enabled TLS",
			host: "127.0.0.1",
			tls:  `{"enabled":true,"certFile":"missing.crt","keyFile":"missing.key"}`,
		},
		{
			name: "loopback ignores missing certificates",
			host: "127.0.0.1",
			tls:  `{"enabled":true}`,
		},
		{
			name: "loopback ignores entire TLS value",
			host: "127.0.0.1",
			tls:  `{"enabled":"ignored","certFile":123,"unknown":true}`,
		},
		{
			name: "default host ignores TLS",
			tls:  `{"enabled":true}`,
		},
		{
			name: "loopback without TLS",
			host: "127.0.0.1",
		},
		{
			name:        "public listener retains TLS",
			host:        "0.0.0.0",
			tls:         `{"enabled":true,"certFile":"server.crt","keyFile":"server.key"}`,
			wantEnabled: true,
		},
		{
			name:        "other host retains TLS behavior",
			host:        "localhost",
			tls:         `{"enabled":true,"certFile":"server.crt","keyFile":"server.key"}`,
			wantEnabled: true,
		},
		{
			name:      "public listener requires certificates",
			host:      "0.0.0.0",
			tls:       `{"enabled":true}`,
			wantError: true,
		},
		{
			name:      "public listener validates TLS types",
			host:      "0.0.0.0",
			tls:       `{"enabled":"invalid"}`,
			wantError: true,
		},
		{
			name:      "public listener rejects unknown TLS fields",
			host:      "0.0.0.0",
			tls:       `{"unknown":true}`,
			wantError: true,
		},
		{
			name: "disabled TLS clears certificate paths",
			host: "0.0.0.0",
			tls:  `{"enabled":false,"certFile":"server.crt","keyFile":"server.key"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := map[string]any{
				"host": tt.host, "port": 8080, "admin": "test-admin", "password": "test-password",
			}
			if tt.tls != "" {
				settings["tls"] = json.RawMessage(tt.tls)
			}
			encoded, err := json.Marshal(map[string]any{"settings": settings})
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := ParseConfig(strings.NewReader(string(encoded)))
			if tt.wantError {
				if err == nil {
					t.Fatal("ParseConfig() accepted invalid TLS configuration")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseConfig() error = %v", err)
			}
			if cfg.Settings.TLS == nil || cfg.Settings.TLS.Enabled != tt.wantEnabled {
				t.Fatalf("TLS = %+v, want enabled = %t", cfg.Settings.TLS, tt.wantEnabled)
			}
			if tt.wantEnabled {
				if cfg.Settings.TLS.CertFile != "server.crt" || cfg.Settings.TLS.KeyFile != "server.key" {
					t.Fatalf("TLS certificate paths changed: %+v", cfg.Settings.TLS)
				}
			} else if cfg.Settings.TLS.CertFile != "" || cfg.Settings.TLS.KeyFile != "" {
				t.Fatalf("disabled TLS retained certificate paths: %+v", cfg.Settings.TLS)
			}
		})
	}
}

func TestNormalizeSettingsDisablesLoopbackTLS(t *testing.T) {
	for _, host := range []string{"127.0.0.1", ""} {
		t.Run("host="+host, func(t *testing.T) {
			settings := &Settings{
				Host: host, Port: 8080, Admin: "test-admin", Password: "test-password",
				TLS: &TLSConfig{Enabled: true, CertFile: "missing.crt"},
			}
			if err := NormalizeSettings(settings); err != nil {
				t.Fatalf("NormalizeSettings() error = %v", err)
			}
			if settings.Host != "127.0.0.1" || *settings.TLS != (TLSConfig{}) {
				t.Fatalf("loopback settings retained TLS: %+v", settings)
			}
		})
	}
}
