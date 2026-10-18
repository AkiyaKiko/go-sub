package lib

import (
	"encoding/json"
	"strings"
	"testing"
)

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
