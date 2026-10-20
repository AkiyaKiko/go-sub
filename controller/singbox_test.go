package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.local/gosub/lib"
	"github.com/gin-gonic/gin"
)

const controllerVLESS = "vless://11111111-1111-4111-8111-111111111111@127.0.0.1:443?encryption=none&security=tls&type=ws&host=example.com&path=%2Fsocket#test"

func singBoxTestServer(t *testing.T) (*Server, *lib.Store) {
	t.Helper()
	temporary := t.TempDir()
	configPath := filepath.Join(temporary, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib.SetConfigPath(configPath)
	store, err := lib.OpenStore(filepath.Join(temporary, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lib.SetDefaultStore(store)
	if err := store.UpdateSubscriptionPath("/private-token"); err != nil {
		t.Fatal(err)
	}
	return NewServer(os.DirFS("..")), store
}

func serveTestRequest(server *Server, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestSingBoxModeSettingsAuthPersistenceAndPublication(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	if response := serveTestRequest(server, "POST", "/singbox-settings"); response.Code != statusSeeOther {
		t.Fatal("settings save did not require login")
	}
	login := httptest.NewRequest("POST", "/login", strings.NewReader("username=test-admin&password=test-password"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loggedIn := httptest.NewRecorder()
	server.router.ServeHTTP(loggedIn, login)
	if loggedIn.Code != statusSeeOther || len(loggedIn.Result().Cookies()) != 1 {
		t.Fatal("test login failed")
	}
	cookie := loggedIn.Result().Cookies()[0]
	t.Cleanup(func() { lib.DeleteSession(cookie.Value) })
	save := func(values url.Values, origin string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("POST", "/singbox-settings", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		return response
	}
	for _, mode := range []string{"mixed", "tun"} {
		response := save(url.Values{"mode": {mode}}, "http://example.com")
		var data DashboardData
		if response.Code != statusOK || json.Unmarshal(response.Body.Bytes(), &data) != nil || data.SingBoxMode != mode || data.SingBoxReport.Status != "ready" {
			t.Fatalf("mode save failed with status %d", response.Code)
		}
		settings, err := lib.GetSettings()
		if err != nil || settings.SingBox == nil || settings.SingBox.Mode != mode || settings.Admin != "test-admin" || settings.Password != "test-password" {
			t.Fatal("mode save did not persist or changed credentials")
		}
		server = NewServer(os.DirFS(".."))
		request := httptest.NewRequest("GET", "/", nil)
		request.AddCookie(cookie)
		page := httptest.NewRecorder()
		server.router.ServeHTTP(page, request)
		if page.Code != statusOK || !strings.Contains(page.Body.String(), `id="open-singbox-template"`) || !strings.Contains(page.Body.String(), `id="singbox-template-dialog"`) {
			t.Fatal("page did not provide the template editor")
		}
		export := serveTestRequest(server, "GET", "/private-token/sing-box")
		var profile struct {
			Inbounds []struct {
				Type string `json:"type"`
			} `json:"inbounds"`
		}
		if export.Code != statusOK || json.Unmarshal(export.Body.Bytes(), &profile) != nil || len(profile.Inbounds) != 1 || profile.Inbounds[0].Type != mode {
			t.Fatal("saved mode was not reflected in public subscription")
		}
		if xray := serveTestRequest(server, "GET", "/private-token"); xray.Body.String() != base64.StdEncoding.EncodeToString([]byte(controllerVLESS)) {
			t.Fatal("mode setting changed Xray subscription")
		}
	}
	for _, values := range []url.Values{{}, {"mode": {"invalid"}}, {"mode": {"mixed", "tun"}}} {
		if response := save(values, ""); response.Code != statusBadRequest {
			t.Fatal("invalid mode was not rejected")
		}
	}
	if response := save(url.Values{"mode": {"mixed"}}, "https://attacker.example"); response.Code != statusForbidden {
		t.Fatal("cross-origin mode write was accepted")
	}
	settings, err := lib.GetSettings()
	if err != nil || settings.SingBox.Mode != "tun" {
		t.Fatal("rejected write modified persisted mode")
	}
	for _, path := range []string{"/singbox-settings", "/singbox-settings/sub"} {
		if err := store.UpdateSubscriptionPath(path); err == nil {
			t.Fatal("subscription path occupied the settings endpoint")
		}
		if err := store.CreateRouteRule("conflict", path, `{}`); err == nil {
			t.Fatal("rule path occupied the settings endpoint")
		}
	}
}

func TestSingBoxSubscriptionAndOriginalXrayBytes(t *testing.T) {
	var accessLog bytes.Buffer
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &accessLog
	defer func() { gin.DefaultWriter = previousWriter }()
	server, store := singBoxTestServer(t)
	nodes := []string{
		controllerVLESS, "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ@127.0.0.1:8388#SS",
		"vmess://11111111-1111-4111-8111-111111111111@127.0.0.1:443?encryption=auto&security=tls",
		"trojan://password@127.0.0.1:443?type=ws&path=%2Fsocket",
		"hy://127.0.0.1?auth=password&upmbps=20&downmbps=100",
		"hy2://password@127.0.0.1:443,5000-5002",
		"tuic://11111111-1111-4111-8111-111111111111:password@127.0.0.1",
		"anytls://password@127.0.0.1",
		"https://user:password@127.0.0.1:8443",
		"socks5://user:password@127.0.0.1:1080",
	}
	if err := store.ReplaceNodes(nodes); err != nil {
		t.Fatal(err)
	}
	xray := serveTestRequest(server, "GET", "/private-token")
	wantXray := base64.StdEncoding.EncodeToString([]byte(strings.Join(nodes, "\n")))
	if xray.Code != statusOK || xray.Body.String() != wantXray || xray.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal("original Xray response changed")
	}
	accessLog.Reset()
	singBox := serveTestRequest(server, "GET", "/private-token/sing-box")
	if singBox.Code != statusOK || singBox.Header().Get("Content-Type") != "application/json; charset=utf-8" || singBox.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sing-box headers/status: %d %v", singBox.Code, singBox.Header())
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(singBox.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"inbounds", "outbounds", "route"} {
		if len(config[field]) == 0 {
			t.Fatalf("missing full profile field %s", field)
		}
	}
	var outbounds []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(config["outbounds"], &outbounds); err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, outbound := range outbounds {
		types[outbound.Type] = true
	}
	for _, protocol := range []string{"vless", "shadowsocks", "vmess", "trojan", "hysteria", "hysteria2", "tuic", "anytls", "http", "socks"} {
		if !types[protocol] {
			t.Fatalf("subscription did not publish %s", protocol)
		}
	}
	head := serveTestRequest(server, "HEAD", "/private-token/sing-box")
	if head.Code != statusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(singBox.Body.Len()) {
		t.Fatal("HEAD did not describe JSON response")
	}
	if response := serveTestRequest(server, "POST", "/private-token/sing-box"); response.Code != statusMethodNotAllowed {
		t.Fatalf("POST status = %d", response.Code)
	}
	if strings.Contains(accessLog.String(), "private-token") {
		t.Fatal("sing-box path token was written to access log")
	}
	if response := serveTestRequest(server, "GET", "/wrong-token/sing-box"); response.Code != statusNotFound {
		t.Fatalf("invalid token status = %d", response.Code)
	}
	if response := serveTestRequest(server, "GET", "/api/config"); response.Code != statusUnauthorized {
		t.Fatal("admin diagnostics became public")
	}
}

func TestSingBoxTemplateAPIAndSave(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	if response := serveTestRequest(server, "GET", "/api/singbox-template"); response.Code != statusUnauthorized {
		t.Fatal("template was exposed without login")
	}
	if response := serveTestRequest(server, "POST", "/singbox-template"); response.Code != statusSeeOther {
		t.Fatal("template save did not require login")
	}
	login := httptest.NewRequest("POST", "/login", strings.NewReader("username=test-admin&password=test-password"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loggedIn := httptest.NewRecorder()
	server.router.ServeHTTP(loggedIn, login)
	cookie := loggedIn.Result().Cookies()[0]
	t.Cleanup(func() { lib.DeleteSession(cookie.Value) })
	get := httptest.NewRequest("GET", "/api/singbox-template", nil)
	get.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.router.ServeHTTP(response, get)
	var payload struct {
		Template string            `json:"template"`
		Presets  map[string]string `json:"presets"`
		Storage  string            `json:"storage"`
		Table    string            `json:"table"`
	}
	if response.Code != statusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || !strings.Contains(payload.Template, "{{nodes}}") || payload.Presets["tun"] == "" || payload.Presets["mixed"] == "" || payload.Storage != "sqlite" || payload.Table != "singbox_template" {
		t.Fatal("editor did not receive local template and presets")
	}
	custom := `{"log":{"level":"error"},"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":1099}],"outbounds":[{"type":"selector","tag":"custom","outbounds":["{{nodes}}"]}],"route":{"final":"custom"},"experimental":{"clash_api":{"secret":"private-template-secret"}}}`
	post := func(raw, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/singbox-template", strings.NewReader(url.Values{"template": {raw}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		request.AddCookie(cookie)
		result := httptest.NewRecorder()
		server.router.ServeHTTP(result, request)
		return result
	}
	if result := post(custom, ""); result.Code != statusOK || strings.Contains(result.Body.String(), "private-template-secret") {
		t.Fatal("template save failed or leaked its secret in dashboard JSON")
	}
	raw, err := store.SingBoxTemplate()
	if err != nil || !strings.Contains(string(raw), "private-template-secret") {
		t.Fatal("template was not persisted locally")
	}
	result := serveTestRequest(server, "GET", "/private-token/sing-box")
	if result.Code != statusOK || !strings.Contains(result.Body.String(), `"listen_port": 1099`) || !strings.Contains(result.Body.String(), `"type": "vless"`) || strings.Contains(result.Body.String(), "{{nodes}}") {
		t.Fatal("subscription did not combine custom template and nodes")
	}
	if result := post(`{"outbounds":`, ""); result.Code != statusBadRequest {
		t.Fatal("invalid template was saved")
	}
	if result := post(`{"outbounds":[{"type":"selector","tag":"loop","outbounds":["loop"]}]}`, ""); result.Code != statusBadRequest {
		t.Fatal("cyclic group was saved")
	}
	if result := post(custom, "https://attacker.example"); result.Code != statusForbidden {
		t.Fatal("cross-origin template save succeeded")
	}
	current, _ := store.SingBoxTemplate()
	if !bytes.Equal(raw, current) {
		t.Fatal("rejected save changed the template")
	}
	if err := store.UpdateSubscriptionPath("/singbox-template"); err == nil {
		t.Fatal("template endpoint was not reserved")
	}
	server = NewServer(os.DirFS(".."))
	if result := serveTestRequest(server, "GET", "/private-token/sing-box"); result.Code != statusOK || !strings.Contains(result.Body.String(), `"listen_port": 1099`) {
		t.Fatal("saved template was lost after server recreation")
	}
}

func TestSingBoxSaveFailureRevocationAndClearing(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	good := serveTestRequest(server, "GET", "/private-token/sing-box").Body.String()
	login := httptest.NewRequest("POST", "/login", strings.NewReader("username=test-admin&password=test-password"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, login)
	if recorder.Code != statusSeeOther {
		t.Fatal("test login failed")
	}
	cookie := recorder.Result().Cookies()[0]
	t.Cleanup(func() { lib.DeleteSession(cookie.Value) })
	post := func(path string, values url.Values) DashboardData {
		t.Helper()
		request := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.router.ServeHTTP(response, request)
		if response.Code != statusOK {
			t.Fatalf("save failed: %d", response.Code)
		}
		var data DashboardData
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	newNodes := controllerVLESS + "\nvmess://secret-password-and-uuid"
	data := post("/nodes", url.Values{"nodes": {newNodes}})
	if data.SingBoxReport.Status != "stale" || !strings.Contains(data.SingBoxStatusText, "上一次") {
		t.Fatal("failed export was reported as synchronized")
	}
	report, _ := json.Marshal(data.SingBoxReport)
	if strings.Contains(string(report), "secret-password") {
		t.Fatal("report leaked source credentials")
	}
	if stale := serveTestRequest(server, "GET", "/private-token/sing-box"); stale.Body.String() != good || stale.Header().Get("X-SingBox-Status") != "stale" {
		t.Fatal("invalid source replaced last-good JSON")
	}
	if xray := serveTestRequest(server, "GET", "/private-token"); xray.Body.String() != base64.StdEncoding.EncodeToString([]byte(newNodes)) {
		t.Fatal("sing-box failure blocked Xray save")
	}
	data = post("/subscription-path", url.Values{"path": {"/changed-token"}})
	if data.SingBoxPath != "/changed-token/sing-box" {
		t.Fatalf("new sing-box path = %q", data.SingBoxPath)
	}
	for _, path := range []string{"/private-token", "/private-token/sing-box"} {
		if response := serveTestRequest(server, "GET", path); response.Code != statusNotFound {
			t.Fatalf("revoked URL remained usable: %s", path)
		}
	}
	if response := serveTestRequest(server, "GET", "/changed-token/sing-box"); response.Code != statusOK {
		t.Fatal("new sing-box URL is unavailable")
	}
	data = post("/nodes", url.Values{"nodes": {"vmess://secret-password-and-uuid"}})
	if data.SingBoxReport.Status != "error" || serveTestRequest(server, "GET", "/changed-token/sing-box").Code != statusServiceUnavailable {
		t.Fatal("removed node credentials remained publicly available")
	}
	data = post("/nodes", url.Values{"nodes": {""}})
	if data.SingBoxReport.Status != "disabled" {
		t.Fatal("cleared subscription not disabled")
	}
	response := serveTestRequest(server, "GET", "/changed-token/sing-box")
	if response.Code != statusServiceUnavailable || strings.Contains(response.Body.String(), "outbounds") || strings.Contains(response.Body.String(), "uuid") {
		t.Fatal("empty subscription exposed old credentials")
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(cookie)
	page := httptest.NewRecorder()
	server.router.ServeHTTP(page, request)
	if page.Code != statusOK || !strings.Contains(page.Body.String(), `id="singbox-url"`) || !strings.Contains(page.Body.String(), `data-copy-target="singbox-url"`) || !strings.Contains(page.Body.String(), "已停用") {
		t.Fatal("dashboard is missing sing-box URL or conversion status")
	}
}

func TestSingBoxFirstInvalidExportReturnsUnavailable(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{"unknown://credential"}); err != nil {
		t.Fatal(err)
	}
	response := serveTestRequest(server, "GET", "/private-token/sing-box")
	if response.Code != statusServiceUnavailable || response.Body.String() != `{"message":"sing-box subscription unavailable"}` {
		t.Fatalf("unexpected failure response: %d %s", response.Code, response.Body.String())
	}
	if response := serveTestRequest(server, "HEAD", "/private-token/sing-box"); response.Code != statusServiceUnavailable || response.Body.Len() != 0 {
		t.Fatal("unavailable HEAD response is incorrect")
	}
}

func TestSingBoxInvalidTemplateDoesNotBlockXrayOrClearing(t *testing.T) {
	server, store := singBoxTestServer(t)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	good := serveTestRequest(server, "GET", "/private-token/sing-box").Body.String()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password","singbox":{"mixed_port":-1}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib.SetConfigPath(path)
	if err := store.ReplaceNodes([]string{controllerVLESS}); err != nil {
		t.Fatal(err)
	}
	if response := serveTestRequest(server, "GET", "/private-token"); response.Code != statusOK {
		t.Fatal("invalid sing-box template blocked Xray")
	}
	if response := serveTestRequest(server, "GET", "/private-token/sing-box"); response.Code != statusOK || response.Body.String() != good {
		t.Fatal("invalid template discarded valid JSON")
	}
	if err := store.ReplaceNodes(nil); err != nil {
		t.Fatal(err)
	}
	if response := serveTestRequest(server, "GET", "/private-token/sing-box"); response.Code != statusServiceUnavailable {
		t.Fatal("invalid template kept cleared credentials alive")
	}
}
