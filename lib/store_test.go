package lib

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	if err := store.UpdateSubscriptionPath("/sub"); err != nil {
		t.Fatalf("UpdateSubscriptionPath() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestNodeLimitsPreserveExistingDataAndSecureDelete(t *testing.T) {
	store := newTestStore(t)
	secret := "secure-delete-regression-credential"
	node := "trojan://" + secret + "@127.0.0.1:443#test"
	if err := store.ReplaceNodes([]string{node}); err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, maxSingBoxNodes+1)
	for i := range tooMany {
		tooMany[i] = "unknown://node"
	}
	for _, nodes := range [][]string{tooMany, {strings.Repeat("x", maxShareURIBytes+1)}} {
		if err := store.ReplaceNodes(nodes); err == nil {
			t.Fatal("oversized nodes accepted")
		}
		current, err := store.Nodes()
		if err != nil || len(current) != 1 || current[0] != node {
			t.Fatal("rejected input changed stored nodes")
		}
	}
	if err := store.ReplaceNodes(nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.dbPath)
	if err != nil || bytes.Contains(raw, []byte(secret)) {
		t.Fatal("removed credentials survived in SQLite free pages")
	}
}

func TestPublicPathNamespaceChangesAreSerialized(t *testing.T) {
	store := newTestStore(t)
	for _, path := range []string{"/race-one", "/race-two/sing-box"} {
		if err := store.UpdateSubscriptionPath("/base"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); results <- store.CreateRouteRule("rule", path, `{}`) }()
		go func() {
			defer wg.Done()
			results <- store.UpdateSubscriptionPath(strings.TrimSuffix(path, "/sing-box"))
		}()
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatal("concurrent writes created overlapping public URLs")
		}
	}
}

func TestCanonicalPathsAndFormattedRuleLimit(t *testing.T) {
	for _, path := range []string{"//evil.example", "/a/../api/config", "/a/./b", "/a//b", "/api%2flogin", "/a\\b", "/a\x00b", "/a\u2028b", "/a/"} {
		if err := ValidateSubscriptionPath(path); err == nil {
			t.Fatalf("ambiguous URL accepted: %q", path)
		}
	}
	if _, err := NormalizeJSONText(`{"data":[` + strings.Repeat("0,", 80000) + `0]}`); err == nil {
		t.Fatal("expanded JSON exceeded persisted size limit")
	}
	for _, raw := range []string{`{"rules":[],"rules":[{}]}`, strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65)} {
		if _, err := NormalizeJSONText(raw); err == nil {
			t.Fatal("ambiguous or deeply nested rule JSON accepted")
		}
	}
}

func TestSingBoxRevokesRemovedOrReplacedNodeCredentials(t *testing.T) {
	store := newTestStore(t)
	for _, replacement := range []string{"unknown://invalid", strings.Replace(testVLESS("type=tcp#original"), testVLESSUUID, "22222222-2222-4222-8222-222222222222", 1)} {
		original := testVLESS("type=tcp#original")
		if err := store.ReplaceNodes([]string{original}); err != nil {
			t.Fatal(err)
		}
		if body, _, err := store.SingBoxSubscription(SingBoxOptions{}); err != nil || len(body) == 0 {
			t.Fatal("initial publication unavailable")
		}
		if err := store.ReplaceNodes([]string{replacement, "unknown://invalid"}); err != nil {
			t.Fatal(err)
		}
		if body, report, err := store.SingBoxSubscription(SingBoxOptions{}); err != nil || len(body) != 0 || report.Status != "error" {
			t.Fatal("revoked credentials survived failed conversion")
		}
		var cached struct{ Body, NodeHashes string }
		if err := store.db.Table("singbox_publication").First(&cached).Error; err != nil || cached.Body != "" || cached.NodeHashes != "" {
			t.Fatal("revoked publication remained in SQLite")
		}
	}
}

func TestRouteRuleCRUD(t *testing.T) {
	store := newTestStore(t)

	if err := store.CreateRouteRule("direct", "/rules/direct", `{"route":"direct"}`); err != nil {
		t.Fatalf("CreateRouteRule() error = %v", err)
	}

	rules, err := store.RouteRules()
	if err != nil {
		t.Fatalf("RouteRules() error = %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("len(RouteRules()) = %d, want 1", len(rules))
	}
	if rules[0].Path != "/rules/direct" {
		t.Fatalf("RouteRules()[0].Path = %q", rules[0].Path)
	}
	if !strings.Contains(rules[0].Content, "\n  ") {
		t.Fatalf("route rule content was not formatted: %q", rules[0].Content)
	}

	found, ok, err := store.RouteRuleByPath("/rules/direct")
	if err != nil {
		t.Fatalf("RouteRuleByPath() error = %v", err)
	}
	if !ok || found.ID != rules[0].ID {
		t.Fatalf("RouteRuleByPath() = (%+v, %t), want saved rule", found, ok)
	}

	if err := store.UpdateRouteRule(rules[0].ID, "proxy", "/rules/proxy", `{"route":"proxy"}`); err != nil {
		t.Fatalf("UpdateRouteRule() error = %v", err)
	}
	if _, ok, err := store.RouteRuleByPath("/rules/direct"); err != nil || ok {
		t.Fatalf("old route path lookup = (_, %t, %v), want not found", ok, err)
	}
	if updated, ok, err := store.RouteRuleByPath("/rules/proxy"); err != nil || !ok || updated.Name != "proxy" {
		t.Fatalf("updated route path lookup = (%+v, %t, %v), want updated rule", updated, ok, err)
	}

	if err := store.DeleteRouteRule(rules[0].ID); err != nil {
		t.Fatalf("DeleteRouteRule() error = %v", err)
	}
	if _, ok, err := store.RouteRuleByPath("/rules/proxy"); err != nil || ok {
		t.Fatalf("deleted route path lookup = (_, %t, %v), want not found", ok, err)
	}
}

func TestRouteRulePathConflicts(t *testing.T) {
	store := newTestStore(t)

	if err := store.CreateRouteRule("conflict", "/sub", `{"ok":true}`); err == nil {
		t.Fatal("CreateRouteRule() with subscription path succeeded, want error")
	}
	if err := store.CreateRouteRule("rule", "/route", `{"ok":true}`); err != nil {
		t.Fatalf("CreateRouteRule() error = %v", err)
	}
	if err := store.CreateRouteRule("dupe", "/route", `{"ok":true}`); err == nil {
		t.Fatal("CreateRouteRule() with duplicate route path succeeded, want error")
	}
	if err := store.UpdateSubscriptionPath("/route"); err == nil {
		t.Fatal("UpdateSubscriptionPath() with route rule path succeeded, want error")
	}
}

func TestValidateSubscriptionPathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"/", "/api/config", "/assets/app.js", "/login", "/route-rules/1", "/sub?x=1", "/sub#part"} {
		if err := ValidateSubscriptionPath(path); err == nil {
			t.Fatalf("ValidateSubscriptionPath(%q) succeeded, want error", path)
		}
	}
}

func TestNormalizeJSONTextRejectsInvalidJSON(t *testing.T) {
	if _, err := NormalizeJSONText(`{"broken"`); err == nil {
		t.Fatal("NormalizeJSONText() accepted invalid JSON")
	}
	if got, err := NormalizeJSONText(""); err != nil || got != DefaultRouteRuleJSON {
		t.Fatalf("NormalizeJSONText(\"\") = (%q, %v), want default JSON", got, err)
	}
}
