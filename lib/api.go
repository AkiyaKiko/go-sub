package lib

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func GetSubscriptionMuxDynamic() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 只允许 GET
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 动态读取当前配置的订阅路径
		settings, err := GetSettings()
		if err != nil || settings == nil || settings.Path == "" {
			http.Error(w, "config error", http.StatusInternalServerError)
			return
		}

		b64_subs, err := GetB64FromLib()
		if err != nil {
			log.Printf("Error getting subscription data: %v", err)
			b64_subs = ""
		}

		ip, port, _ := net.SplitHostPort(r.RemoteAddr)
		log.Printf(
			`time=%s src=%s:%s method=%s path=%q ua=%q referer=%q lang=%q`,
			time.Now().Format("2006-01-02 15:04:05"), ip, port,
			r.Method,
			r.URL.RequestURI(),
			r.Header.Get("User-Agent"),
			r.Header.Get("Referer"),
			r.Header.Get("Accept-Language"),
		)

		if r.URL.Path != settings.Path {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(b64_subs))
	})

	return mux
}

func ProtectedWrapper(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		sessionData, ok := GetSession(cookie.Value)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "sData", sessionData)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})

}

func GetWebAPIMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Read all current config for admin panel
	mux.Handle("/api/config",
		ProtectedWrapper(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}

				settings, err := GetSettings()
				if err != nil {
					http.Error(w, "Failed to load settings", http.StatusInternalServerError)
					return
				}
				nodes, err := GetNodes()
				if err != nil {
					nodes = []string{}
				}

				scheme := "http"
				if settings.TLS != nil && settings.TLS.Enabled {
					scheme = "https"
				}
				subURL := scheme + "://" + net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port)) + settings.Path

				writeJSON(w, http.StatusOK, map[string]any{
					"settings":         settings,
					"nodes":            nodes,
					"subscription_url": subURL,
				})
			})))

	// Change Settings.sub uri
	mux.Handle("/api/change_uri",
		ProtectedWrapper(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
				if err := r.ParseForm(); err != nil {
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}
				newUri := strings.TrimSpace(r.FormValue("uri"))
				if newUri == "" || !strings.HasPrefix(newUri, "/") {
					http.Error(w, "Invalid uri: must start with '/'", http.StatusBadRequest)
					return
				}
				if !ChangeSubUri(newUri) {
					http.Error(w, "Failed to change URI", http.StatusInternalServerError)
					return
				}
				if err := PersistConfigToFile(); err != nil {
					http.Error(w, "Failed to persist config", http.StatusInternalServerError)
					return
				}
				log.Printf("[%s] Subscription URI changed to: http://%s:%d%s", time.Now().Format("2006-01-02 15:04:05"), GLOBAL_CONFIG.Settings.Host, GLOBAL_CONFIG.Settings.Port, newUri)
				w.WriteHeader(http.StatusOK)
			})))

	// Change Nodes
	mux.Handle("/api/change_nodes",
		ProtectedWrapper(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
				if err := r.ParseForm(); err != nil {
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}
				newNodes := r.Form["nodes"]
				if !ChangeNodes(newNodes) {
					http.Error(w, "Failed to change nodes", http.StatusInternalServerError)
					return
				}
				if err := PersistConfigToFile(); err != nil {
					http.Error(w, "Failed to persist config", http.StatusInternalServerError)
					return
				}
				log.Printf("[%s] Nodes updated! ", time.Now().Format("2006-01-02 15:04:05"))
				w.WriteHeader(http.StatusOK)
			})))

	return mux
}

func GetLoginAPIMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/api/session", ProtectedWrapper(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sessionData, ok := r.Context().Value("sData").(SessionData)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"user_id":       sessionData.UserID,
		})
	})))

	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		password := r.FormValue("password")
		if !Authenticate(username, password) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		settings, err := GetSettings()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		sid, err := GenerateSessionID()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		expiration := time.Now().Add(2160 * time.Hour)

		userID, err := randUserID()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		SetSession(sid, SessionData{UserID: userID, ExpiresAt: expiration})
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    sid,
			Expires:  expiration,
			HttpOnly: true,
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
			Secure:   settings.TLS != nil && settings.TLS.Enabled,
		})
		w.WriteHeader(http.StatusOK)
	})

	mux.Handle("/api/logout", ProtectedWrapper(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cookie, err := r.Cookie("session_id")
		if err == nil && cookie.Value != "" {
			DeleteSession(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: true,
		})
		w.WriteHeader(http.StatusOK)
	})))

	return mux
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
