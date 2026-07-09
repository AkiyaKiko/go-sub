package lib

import (
	"path/filepath"
	"strings"
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
