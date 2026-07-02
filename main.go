package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"git.local/gosub/lib"
)

const (
	configPath = "config.json"
	dbPath     = "gosub.db"
)

func NewDefaultConfig() *lib.Config {
	randomStr, err := lib.RandomString(8)
	if err != nil {
		log.Fatal(err)
	}
	return &lib.Config{
		Settings: &lib.Settings{
			Host:     "127.0.0.1",
			Port:     8080,
			Admin:    fmt.Sprintf("admin_%s", randomStr),
			Password: fmt.Sprintf("password_%s", randomStr),
			TLS:      &lib.TLSConfig{Enabled: false},
		},
	}
}

func WriteDefaultConfig(cfg *lib.Config) error {
	encoded, err := lib.EncodeConfig(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, encoded, 0644)
}

func InitializeRuntime() (*lib.Settings, string) {
	lib.SetConfigPath(configPath)

	if err := ensureConfigFile(); err != nil {
		log.Fatal(err)
	}
	log.Println("initializing sqlite data")
	if err := lib.InitializeStore(dbPath); err != nil {
		log.Fatal(err)
	}

	settings, err := lib.GetSettings()
	if err != nil {
		log.Fatal(err)
	}
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		log.Fatal(err)
	}
	return settings, path
}

func ensureConfigFile() error {
	if err := validateConfigFile(); errors.Is(err, os.ErrNotExist) {
		if err := WriteDefaultConfig(NewDefaultConfig()); err != nil {
			return fmt.Errorf("failed to write default config: %w", err)
		}
		return validateConfigFile()
	} else if err != nil {
		return err
	}
	return nil
}

func validateConfigFile() error {
	f, err := os.Open(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = lib.ParseConfig(f)
	return err
}

func main() {
	settings, path := InitializeRuntime()
	server := NewServer()
	printStartupInfo(settings, path)

	addr := net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port))
	if settings.TLS != nil && settings.TLS.Enabled {
		log.Fatal(ListenAndServeTLSWithSamePortRedirect(addr, settings.TLS.CertFile, settings.TLS.KeyFile, server.Handler()))
	}
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}

func printStartupInfo(settings *lib.Settings, path string) {
	webURL, subscriptionURL := startupURLs(settings, path)
	tlsEnabled := settings.TLS != nil && settings.TLS.Enabled

	log.Println("gosub started")
	log.Printf("web_url=%s", webURL)
	log.Printf("subscription_url=%s", subscriptionURL)
	log.Printf("subscription_path=%s", path)
	log.Printf("listen=%s", net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port)))
	log.Printf("admin=%s", settings.Admin)
	log.Printf("password=%s", settings.Password)
	log.Printf("tls_enabled=%t", tlsEnabled)
	if tlsEnabled {
		log.Printf("tls_cert_file=%s", settings.TLS.CertFile)
		log.Printf("tls_key_file=%s", settings.TLS.KeyFile)
	}
}
