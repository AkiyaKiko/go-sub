package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"git.local/gosub/lib"
)

const CONFIG_PATH = "config.json"

func NewDefaultConfig() *lib.Config {
	randomStr, err := lib.RandomString(8)
	if err != nil {
		log.Fatal(err)
	}
	return &lib.Config{
		Settings: &lib.Settings{
			Host:     "127.0.0.1",
			Port:     8080,
			Path:     fmt.Sprintf(`/%v`, randomStr),
			Admin:    fmt.Sprintf(`admin_%v`, randomStr),
			Password: fmt.Sprintf(`password_%v`, randomStr),
			TLS:      &lib.TLSConfig{Enabled: false},
		},
		Nodes: []string{},
	}
}

func WriteDefaultConfig(d_cfg *lib.Config) error {
	encoded, err := lib.EncodeConfig(d_cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(CONFIG_PATH, encoded, 0644)
}

func InitializeConfig() {
	// If not exits, create a default config file and exit
	_, err := os.Stat(CONFIG_PATH)
	if err != nil {
		if os.IsNotExist(err) {
			if wErr := WriteDefaultConfig(NewDefaultConfig()); wErr != nil {
				log.Fatal(fmt.Errorf("failed to write default config: %w", wErr))
			}
			log.Fatal("Missing config.json! The file has been created but still needs to be edited.")
		}
		log.Fatal(fmt.Errorf("stat config.json failed: %w", err))
	}

	f, fErr := os.Open(CONFIG_PATH)
	if fErr != nil {
		log.Fatal(fmt.Errorf("failed to open config.json: %w", fErr))
	}
	defer f.Close()

	if err := lib.ParseConfig(f); err != nil {
		log.Fatal(err)
	}
}

func main() {
	lib.SetConfigPath(CONFIG_PATH)
	InitializeConfig()

	settings, err := lib.GetSettings()
	if err != nil {
		log.Fatal(err)
	}

	subMux := lib.GetSubscriptionMuxDynamic()
	apiMux := lib.GetWebAPIMux()
	loginMux := lib.GetLoginAPIMux()
	webHandler, err := NewEmbeddedWebHandler()
	if err != nil {
		log.Fatal(fmt.Errorf("failed to initialize embedded web handler: %w", err))
	}

	root := http.NewServeMux()
	root.Handle("/api/", apiMux)
	root.Handle("/api/login", loginMux)
	root.Handle("/api/session", loginMux)
	root.Handle("/api/logout", loginMux)
	root.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentSettings, err := lib.GetSettings()
		if err != nil {
			http.Error(w, "config error", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == currentSettings.Path {
			subMux.ServeHTTP(w, r)
			return
		}
		webHandler.ServeHTTP(w, r)
	}))

	addr := net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port))

	if settings.TLS.Enabled {
		log.Printf("Starting server at https://%s%s", addr, settings.Path)
		log.Fatal(ListenAndServeTLSWithSamePortRedirect(addr, settings.TLS.CertFile, settings.TLS.KeyFile, root))
	} else {
		log.Printf("Starting server at http://%s%s", addr, settings.Path)
		log.Fatal(http.ListenAndServe(addr, root))
	}
}
