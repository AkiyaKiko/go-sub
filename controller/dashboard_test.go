package controller

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"git.local/gosub/lib"
	"golang.org/x/net/html"
)

func TestLoopbackDashboardRouteRulePaths(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"settings":{
		"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password",
		"tls":{"enabled":true}
	}}`), 0600); err != nil {
		t.Fatal(err)
	}
	lib.SetConfigPath(configPath)
	store, err := lib.OpenStore(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lib.SetDefaultStore(store)
	if err := store.UpdateSubscriptionPath("/sub"); err != nil {
		t.Fatal(err)
	}
	server := NewServer(os.DirFS(".."))
	login := httptest.NewRequest("POST", "/login", strings.NewReader("username=test-admin&password=test-password"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRecorder := httptest.NewRecorder()
	server.router.ServeHTTP(loginRecorder, login)
	if loginRecorder.Code != statusSeeOther {
		t.Fatalf("loopback login status = %d, want %d", loginRecorder.Code, statusSeeOther)
	}
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("loopback session cookie = %+v, want one HTTP cookie", cookies)
	}
	cookie := cookies[0]
	t.Cleanup(func() { lib.DeleteSession(cookie.Value) })
	assertPaths := func(want ...string) {
		t.Helper()
		request := httptest.NewRequest("GET", "/", nil)
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.router.ServeHTTP(recorder, request)
		if recorder.Code != statusOK {
			t.Fatalf("dashboard status = %d, want %d", recorder.Code, statusOK)
		}
		if recorder.Header().Get("Strict-Transport-Security") != "" {
			t.Fatal("loopback HTTP dashboard unexpectedly enabled HSTS")
		}
		assertDashboardRouteRuleLayout(t, recorder.Body.String(), len(want) == 0)
		if got := dashboardRouteRulePaths(t, recorder.Body.String()); !reflect.DeepEqual(got, want) {
			t.Fatalf("current service paths = %v, want %v", got, want)
		}
	}
	post := func(path string, values url.Values) DashboardData {
		t.Helper()
		request := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		server.router.ServeHTTP(recorder, request)
		if recorder.Code != statusOK {
			t.Fatalf("POST %s status = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		var data DashboardData
		if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.TLSStatus != "关闭" {
			t.Fatalf("dashboard TLS status = %q, want disabled", data.TLSStatus)
		}
		return data
	}

	assertPaths()
	post("/route-rules", url.Values{"name": {"first"}, "path": {"/rules/first"}, "content": {`{}`}})
	assertPaths("/rules/first")
	data := post("/route-rules", url.Values{"name": {"second"}, "submit_action": {"random"}})
	if len(data.RouteRules) != 2 {
		t.Fatalf("rule count = %d, want 2", len(data.RouteRules))
	}
	firstURL := "/route-rules/" + strconv.FormatUint(uint64(data.RouteRules[0].ID), 10)
	secondURL := "/route-rules/" + strconv.FormatUint(uint64(data.RouteRules[1].ID), 10)
	randomPath := data.RouteRules[1].Path
	if randomPath == "" || !strings.HasPrefix(randomPath, "/") {
		t.Fatalf("random route path = %q", randomPath)
	}
	assertPaths("/rules/first", randomPath)
	post(firstURL, url.Values{"name": {"updated"}, "path": {"/rules/updated"}})
	assertPaths("/rules/updated", randomPath)
	post(secondURL+"/delete", nil)
	assertPaths("/rules/updated")
	post(firstURL+"/delete", nil)
	assertPaths()
}

func assertDashboardRouteRuleLayout(t *testing.T, body string, empty bool) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	attr := func(node *html.Node, key string) string {
		for _, entry := range node.Attr {
			if entry.Key == key {
				return entry.Val
			}
		}
		return ""
	}
	nodes := make(map[string]*html.Node)
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if id := attr(node, "id"); id != "" {
			nodes[id] = node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	for _, prefix := range []string{"new", "edit"} {
		form := nodes[prefix+"-route-rule-form"]
		dialog := nodes[prefix+"-route-rule-dialog"]
		if form == nil || dialog == nil || dialog.Data != "dialog" || form.Parent != dialog {
			t.Fatalf("%s route rule form must be inside its dialog", prefix)
		}
		for _, entry := range dialog.Attr {
			if entry.Key == "open" {
				t.Fatalf("%s route rule dialog must be closed initially", prefix)
			}
		}
	}
	for _, id := range []string{"route-rule-select", "open-edit-route-rule", "delete-route-rule-button", "copy-route-rule-url"} {
		node := nodes[id]
		if node == nil {
			t.Fatalf("missing route rule control %s", id)
		}
		disabled := false
		for _, entry := range node.Attr {
			disabled = disabled || entry.Key == "disabled"
		}
		if disabled != empty {
			t.Fatalf("%s disabled = %v, want %v", id, disabled, empty)
		}
	}
	if nodes["route-rule-url"] == nil || nodes["edit-route-rule-url"] == nil || attr(nodes["copy-route-rule-url"], "data-copy-target") != "route-rule-url" {
		t.Fatal("route rule URL display and copy target are missing")
	}
	if nodes["route-rule-editor"] != nil {
		t.Fatal("inline route rule editor must not be rendered")
	}
}

func dashboardRouteRulePaths(t *testing.T, body string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var list *html.Node
	var findList func(*html.Node)
	findList = func(node *html.Node) {
		for _, attr := range node.Attr {
			if attr.Key == "id" && attr.Val == "route-rule-paths" {
				list = node
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			findList(child)
		}
	}
	findList(doc)
	if list == nil {
		t.Fatal("current service route rule paths are missing")
	}
	var paths []string
	for item := list.FirstChild; item != nil; item = item.NextSibling {
		for child := item.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "code" && child.FirstChild != nil {
				paths = append(paths, child.FirstChild.Data)
			}
		}
	}
	return paths
}
