package controller

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.local/gosub/lib"
)

func TestRouteRulePathReturnsJSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "settings": {
    "host": "127.0.0.1",
    "port": 12345,
    "admin": "admin",
    "password": "password",
    "tls": {
      "enabled": false,
      "certFile": "",
      "keyFile": ""
    }
  }
}`), 0644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	lib.SetConfigPath(configPath)

	store, err := lib.OpenStore(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	lib.SetDefaultStore(store)

	if err := store.UpdateSubscriptionPath("/sub"); err != nil {
		t.Fatalf("UpdateSubscriptionPath() error = %v", err)
	}
	if err := store.CreateRouteRule("rule", "/route-rule", `{"rules":[{"domain":"example.com"}]}`); err != nil {
		t.Fatalf("CreateRouteRule() error = %v", err)
	}

	server := NewServer(os.DirFS(".."))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/route-rule", nil)
	server.router.ServeHTTP(recorder, request)

	if recorder.Code != statusOK {
		t.Fatalf("GET /route-rule status = %d, want %d", recorder.Code, statusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"rules"`) || !strings.Contains(body, `"example.com"`) {
		t.Fatalf("body = %q, want route rule JSON", body)
	}
}
