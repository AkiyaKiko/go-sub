package controller

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

type securityReadCounter struct{ reads int }

func (body *securityReadCounter) Read([]byte) (int, error) { body.reads++; return 0, io.EOF }
func (body *securityReadCounter) Close() error             { return nil }

func TestUnauthenticatedWritesDoNotParseLargeBodies(t *testing.T) {
	server, _ := singBoxTestServer(t)
	body := &securityReadCounter{}
	request := httptest.NewRequest("POST", "/nodes", nil)
	request.Body = body
	request.ContentLength = 1024 * 1024
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.router.ServeHTTP(response, request)
	if response.Code != statusSeeOther || body.reads != 0 {
		t.Fatal("unauthenticated body was parsed before authentication")
	}
}

func TestConcurrentWritesAreBounded(t *testing.T) {
	server, _ := singBoxTestServer(t)
	cookie := securityLogin(t, server, nil)
	entered, release := make(chan struct{}, 4), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	server.router.POST("/test-write-limit", func(c *gin.Context) { entered <- struct{}{}; <-release; c.Status(statusOK) })
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/test-write-limit", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, r)
		return w
	}
	var wg sync.WaitGroup
	for index := 0; index < 4; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if request().Code != statusOK {
				t.Error("admitted write failed")
			}
		}()
	}
	for index := 0; index < 4; index++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("writes did not enter bounded handler")
		}
	}
	if request().Code != statusTooManyRequests {
		t.Fatal("excess concurrent writes were not rejected")
	}
	once.Do(func() { close(release) })
	wg.Wait()
}

func TestExplicitlyDisabledProxyTrust(t *testing.T) {
	_, _ = singBoxTestServer(t)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password","trusted_proxies":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib.SetConfigPath(path)
	server := NewServer(os.DirFS(".."))
	server.router.GET("/test-client-ip", func(c *gin.Context) { c.String(statusOK, c.ClientIP()) })
	request := httptest.NewRequest("GET", "/test-client-ip", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.10")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	server.router.ServeHTTP(response, request)
	if response.Body.String() != "127.0.0.1" || response.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("disabled proxy trust was still used")
	}
}

func TestSameOriginProtectionRejectsCrossOriginWrite(t *testing.T) {
	server := NewServer(os.DirFS(".."))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "http://127.0.0.1/api/login", nil)
	request.Header.Set("Origin", "http://evil.example")

	server.router.ServeHTTP(recorder, request)

	if recorder.Code != statusForbidden {
		t.Fatalf("cross-origin POST status = %d, want %d", recorder.Code, statusForbidden)
	}
}

func TestNoReferrerSameOriginFormLogin(t *testing.T) {
	server, _ := singBoxTestServer(t)
	page := serveTestRequest(server, "GET", "/login")
	if page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("test must exercise the no-referrer login page")
	}
	for _, origin := range []string{"null", ""} {
		request := httptest.NewRequest("POST", "http://127.0.0.1:12345/login", strings.NewReader("username=test-admin&password=test-password"))
		request.RemoteAddr = "127.0.0.1:54321"
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("Sec-Fetch-Mode", "navigate")
		request.Header.Set("Sec-Fetch-Dest", "document")
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		if response.Code != statusSeeOther || response.Header().Get("Location") != "/?msg=login_ok" || len(response.Result().Cookies()) != 1 {
			t.Fatalf("same-origin no-referrer form with Origin=%q failed: status=%d", origin, response.Code)
		}
		cookie := response.Result().Cookies()[0]
		t.Cleanup(func() { lib.DeleteSession(cookie.Value) })
		dashboard := httptest.NewRequest("GET", "http://127.0.0.1:12345/", nil)
		dashboard.AddCookie(cookie)
		result := httptest.NewRecorder()
		server.router.ServeHTTP(result, dashboard)
		if result.Code != statusOK || !strings.Contains(result.Body.String(), `id="open-singbox-template"`) {
			t.Fatal("login cookie did not authenticate the dashboard")
		}
	}
}

func securityLogin(t *testing.T, server *Server, cookie *http.Cookie) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/login", strings.NewReader("username=test-admin&password=test-password"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.router.ServeHTTP(response, request)
	if response.Code != statusOK || len(response.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %d", response.Code)
	}
	session := response.Result().Cookies()[0]
	t.Cleanup(func() { lib.DeleteSession(session.Value) })
	return session
}

func TestProxyTrustAndSecureCookies(t *testing.T) {
	server, _ := singBoxTestServer(t)
	server.router.GET("/test-client-ip", func(c *gin.Context) { c.String(statusOK, c.ClientIP()) })
	for _, test := range []struct {
		name, remote, proto, origin string
		tls, secure                 bool
		ip                          string
	}{
		{"direct HTTP ignores forged headers", "198.51.100.10:1234", "https", "http://panel.example:25363", false, false, "198.51.100.10"},
		{"trusted HTTPS proxy", "127.0.0.1:1234", "https", "https://panel.example:25363", false, true, "203.0.113.10"},
		{"native HTTPS", "198.51.100.10:1234", "http", "https://panel.example:25363", true, true, "198.51.100.10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "http://panel.example:25363/api/login", strings.NewReader("username=test-admin&password=test-password"))
			request.RemoteAddr = test.remote
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", test.origin)
			request.Header.Set("X-Forwarded-Proto", test.proto)
			request.Header.Set("X-Forwarded-For", "203.0.113.10")
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			response := httptest.NewRecorder()
			server.router.ServeHTTP(response, request)
			if response.Code != statusOK || len(response.Result().Cookies()) != 1 {
				t.Fatalf("login failed: %d", response.Code)
			}
			cookie := response.Result().Cookies()[0]
			defer lib.DeleteSession(cookie.Value)
			if cookie.Secure != test.secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != sessionMaxAge {
				t.Fatal("unsafe session cookie flags")
			}
			if (response.Header().Get("Strict-Transport-Security") != "") != test.secure {
				t.Fatal("HSTS trusted an incorrect scheme")
			}
			ipRequest := httptest.NewRequest("GET", "/test-client-ip", nil)
			ipRequest.RemoteAddr = test.remote
			ipRequest.Header.Set("X-Forwarded-For", "203.0.113.10")
			ipResponse := httptest.NewRecorder()
			server.router.ServeHTTP(ipResponse, ipRequest)
			if ipResponse.Body.String() != test.ip {
				t.Fatalf("untrusted client IP: %s", ipResponse.Body.String())
			}
		})
	}
}

func TestStrictOriginAndFetchMetadata(t *testing.T) {
	server, _ := singBoxTestServer(t)
	for _, test := range []struct {
		origin, referer, fetch string
		allowed                bool
	}{
		{"http://panel.example", "", "same-origin", true},
		{"http://panel.example:80", "", "same-origin", true},
		{"https://panel.example", "", "", false},
		{"http://panel.example:81", "", "", false},
		{"http://user@panel.example", "", "", false},
		{"null", "", "", false},
		{"null", "", "same-origin", true},
		{"", "", "same-origin", true},
		{"null", "", "same-site", false},
		{"null", "", "cross-site", false},
		{"http://panel.example:81", "", "same-origin", false},
		{"http://panel.example/path", "", "", false},
		{"http://panel.example", "", "cross-site", false},
		{"", "http://panel.example/page", "same-origin", true},
		{"", "https://panel.example/page", "", false},
		{"", "", "same-site", false},
		{"", "", "", true},
	} {
		request := httptest.NewRequest("POST", "http://panel.example/api/login", nil)
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Referer", test.referer)
		request.Header.Set("Sec-Fetch-Site", test.fetch)
		request.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		if (response.Code != statusForbidden) != test.allowed {
			t.Fatalf("origin=%q referer=%q fetch=%q status=%d", test.origin, test.referer, test.fetch, response.Code)
		}
	}
}

func TestLoginLimitsShareWebAndAPIAndIgnoreSpoofedIP(t *testing.T) {
	server, _ := singBoxTestServer(t)
	for index := 0; index < 11; index++ {
		path := "/api/login"
		if index%2 == 1 {
			path = "/login"
		}
		request := httptest.NewRequest("POST", path, nil)
		request.Header.Set("X-Forwarded-For", "203.0.113."+strings.Repeat("1", index+1))
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		if index < 10 && response.Code == statusTooManyRequests {
			t.Fatal("rate limit fired too early")
		}
		if index == 10 && (response.Code != statusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatal("login brute force was not limited")
		}
	}
	limiter := loginLimiter{}
	now := time.Now()
	for index := 0; index < 10; index++ {
		if !limiter.allow("client", now) {
			t.Fatal("limiter rejected initial attempts")
		}
	}
	if limiter.allow("client", now) || !limiter.allow("client", now.Add(5*time.Minute)) {
		t.Fatal("limiter window did not expire")
	}
}

func TestWriteBodyRejectionPreservesData(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	cookie := securityLogin(t, server, nil)
	for _, test := range []struct {
		path, body, contentType string
		status                  int
		chunked                 bool
	}{
		{"/nodes", "nodes=%ZZ", "application/x-www-form-urlencoded", statusBadRequest, false},
		{"/nodes", "nodes=one&nodes=two", "application/x-www-form-urlencoded", statusBadRequest, false},
		{"/nodes", `{"nodes":[]}`, "application/json", statusUnsupportedMediaType, false},
		{"/nodes", "nodes=one", "multipart/form-data; boundary=test", statusUnsupportedMediaType, false},
		{"/api/login", "username=" + strings.Repeat("x", 9*1024), "application/x-www-form-urlencoded", statusRequestTooLarge, false},
		{"/api/login", "username=" + strings.Repeat("x", 9*1024), "application/x-www-form-urlencoded", statusRequestTooLarge, true},
	} {
		request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", test.contentType)
		if test.chunked {
			request.ContentLength = -1
		}
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("body rejection: %s status=%d want=%d", test.path, response.Code, test.status)
		}
	}
	nodes, err := store.Nodes()
	if err != nil || len(nodes) != 1 || nodes[0] != controllerVLESS {
		t.Fatal("bad body destroyed nodes")
	}
}

func TestSessionRotationCredentialRevocationAndConfigRedaction(t *testing.T) {
	server, _ := singBoxTestServer(t)
	old := securityLogin(t, server, nil)
	current := securityLogin(t, server, old)
	request := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, r)
		return w
	}
	if old.Value == current.Value || request("/api/session", old).Code != statusUnauthorized {
		t.Fatal("old session survived rotation")
	}
	config := request("/api/config", current)
	if config.Code != statusOK || strings.Contains(config.Body.String(), "test-password") {
		t.Fatal("config API exposed administrator password")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"changed-password"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib.SetConfigPath(path)
	if request("/api/session", current).Code != statusUnauthorized {
		t.Fatal("password change did not revoke sessions")
	}
}

func TestPrivateURLsAndPanicDetailsAreNotLogged(t *testing.T) {
	var output bytes.Buffer
	writer, logger := gin.DefaultWriter, log.Writer()
	gin.DefaultWriter = &output
	log.SetOutput(&output)
	t.Cleanup(func() { gin.DefaultWriter = writer; log.SetOutput(logger) })
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRouteRule("test", "/secret-rule-token", `{}`); err != nil {
		t.Fatal(err)
	}
	server.router.GET("/test-panic", func(c *gin.Context) { panic("secret-panic-value") })
	for _, path := range []string{"/private-token?password=secret-query", "/private-token/sing-box", "/secret-rule-token", "/test-panic?password=secret-query"} {
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("Cookie", "session_id=secret-cookie")
		request.Header.Set("Referer", "https://example.com/secret-referer")
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
	}
	for _, secret := range []string{"private-token", "secret-rule-token", "secret-query", "secret-panic-value", "secret-cookie", "secret-referer"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("sensitive value entered logs: %s", secret)
		}
	}
}

func TestHTTPTimeoutsAndStoredTextEscaping(t *testing.T) {
	httpServer := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if httpServer.ReadHeaderTimeout <= 0 || httpServer.ReadTimeout <= 0 || httpServer.WriteTimeout <= 0 || httpServer.IdleTimeout <= 0 || httpServer.MaxHeaderBytes > 32*1024 {
		t.Fatal("unbounded HTTP server")
	}
	server, store := singBoxTestServer(t)
	payload := `</textarea><script>alert("stored-xss")</script>`
	if err := store.ReplaceNodes([]string{payload}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRouteRule(payload, "/xss-test", `{"value":"<img src=x onerror=alert(1)>"}`); err != nil {
		t.Fatal(err)
	}
	cookie := securityLogin(t, server, nil)
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.router.ServeHTTP(response, request)
	if response.Code != statusOK || strings.Contains(response.Body.String(), payload) || strings.Contains(response.Body.String(), "<img src=x") || response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("stored content was not escaped")
	}
}

func TestSameOriginProtectionAllowsSameOriginWrite(t *testing.T) {
	server := NewServer(os.DirFS(".."))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "http://127.0.0.1/api/login", nil)
	request.Header.Set("Origin", "http://127.0.0.1")

	server.router.ServeHTTP(recorder, request)

	if recorder.Code == statusForbidden {
		t.Fatalf("same-origin POST was forbidden")
	}
}
