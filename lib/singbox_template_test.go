package lib

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"git.local/gosub/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOldSQLiteSchemaMigratesWithoutLosingExistingData(t *testing.T) {
	for _, legacySettings := range []bool{false, true} {
		name := "subscription table"
		if legacySettings {
			name = "legacy settings path"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			configPath := filepath.Join(directory, "config.json")
			if err := os.WriteFile(configPath, []byte(`{"settings":{"host":"127.0.0.1","port":8080,"admin":"test-admin","password":"test-password","singbox":{"mode":"mixed"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			configFileMu.RLock()
			previous := configFilePath
			configFileMu.RUnlock()
			SetConfigPath(configPath)
			t.Cleanup(func() { SetConfigPath(previous) })
			path := filepath.Join(directory, "production-fixture.db")
			legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if err := legacy.AutoMigrate(&model.Node{}, &model.RouteRule{}); err != nil {
				t.Fatal(err)
			}
			if legacySettings {
				if err := legacy.Exec("CREATE TABLE settings (id integer PRIMARY KEY, path text)").Error; err != nil {
					t.Fatal(err)
				}
				if err := legacy.Exec("INSERT INTO settings(id,path) VALUES(?,?)", 1, "/production-token").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := legacy.AutoMigrate(&model.Subscription{}); err != nil {
					t.Fatal(err)
				}
				if err := legacy.Create(&model.Subscription{ID: 1, Path: "/production-token"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := legacy.Exec("CREATE TABLE singbox_publication (id integer PRIMARY KEY, source_hash text, published_hash text, body text, report_json text, generated_at datetime, updated_at datetime)").Error; err != nil {
				t.Fatal(err)
			}
			if err := legacy.Exec("INSERT INTO singbox_publication(id,body) VALUES(?,?)", 1, "old-cache").Error; err != nil {
				t.Fatal(err)
			}
			node := testVLESS("type=tcp#production-node")
			if err := legacy.Create(&model.Node{Value: node, Position: 0}).Error; err != nil {
				t.Fatal(err)
			}
			rule := model.RouteRule{Name: "production-rule", Path: "/production-route", Content: `{"rules":[]}`}
			if err := legacy.Create(&rule).Error; err != nil {
				t.Fatal(err)
			}
			oldDB, err := legacy.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := oldDB.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if path, _, err := store.EnsureSubscriptionPath(); err != nil || path != "/production-token" {
				t.Fatal("upgrade changed subscription path")
			}
			if !store.db.Migrator().HasTable(&model.SingBoxTemplate{}) || !store.db.Migrator().HasColumn(&model.SingBoxPublication{}, "NodeHashes") {
				t.Fatal("new tables or columns were not migrated")
			}
			initial, err := store.SingBoxTemplate()
			if err != nil || !bytes.Contains(initial, []byte(`"type": "mixed"`)) {
				t.Fatal("old database did not receive its initial preset")
			}
			custom := []byte(`{"log":{"level":"error"},"outbounds":[{"type":"selector","tag":"custom","outbounds":["{{nodes}}"]}],"route":{"final":"custom"}}`)
			if err := store.SaveSingBoxTemplate(custom); err != nil {
				t.Fatal(err)
			}
			saved, err := store.SingBoxTemplate()
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			loaded, err := reopened.SingBoxTemplate()
			if err != nil || !bytes.Equal(loaded, saved) {
				t.Fatal("repeated migration overwrote saved template")
			}
			nodes, err := reopened.Nodes()
			if err != nil || len(nodes) != 1 || nodes[0] != node {
				t.Fatal("migration changed existing nodes")
			}
			rules, err := reopened.RouteRules()
			if err != nil || len(rules) != 1 || rules[0].ID != rule.ID || rules[0].Path != rule.Path || rules[0].Content != rule.Content {
				t.Fatal("migration changed existing routing rules")
			}
			if path, _, err := reopened.EnsureSubscriptionPath(); err != nil || path != "/production-token" {
				t.Fatal("repeated migration changed token")
			}
			body, report, err := reopened.SingBoxSubscription(SingBoxOptions{})
			if err != nil || report.Status != "ready" || !bytes.Contains(body, []byte("production-node")) || !bytes.Contains(body, []byte(`"final": "custom"`)) {
				t.Fatal("migrated nodes and template did not produce subscription")
			}
			var count int64
			if err := reopened.db.Model(&model.SingBoxTemplate{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("repeated upgrade duplicated template records")
			}
		})
	}
}

func TestTemplateDatabaseWriteFailurePreservesPreviousValue(t *testing.T) {
	store := newTestStore(t)
	initial, err := store.SingBoxTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Exec("CREATE TRIGGER reject_template_update BEFORE UPDATE ON singbox_template BEGIN SELECT RAISE(ABORT, 'test write rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSingBoxTemplate([]byte(`{"log":{"level":"debug"}}`)); err == nil {
		t.Fatal("failed database save reported success")
	}
	current, err := store.SingBoxTemplate()
	if err != nil || !bytes.Equal(current, initial) {
		t.Fatal("failed SQLite upsert destroyed previous template")
	}
}

func FuzzSingBoxTemplate(f *testing.F) {
	for _, seed := range []string{`{}`, singBoxTUNPreset, `{"outbounds":[{"type":"selector","tag":"loop","outbounds":["loop"]}]}`, `{"dns":{"servers":[null]}}`, `{"route":{"final":[]}}`, `{"outbounds":null}`, `{"future":{"key":1,"key":2}}`, strings.Repeat("[", 70)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > maxSingBoxTemplateBytes {
			t.Skip()
		}
		_, _ = BuildSingBoxConfigWithTemplate([]string{testVLESS("type=tcp#test")}, SingBoxOptions{}, []byte(raw))
	})
}

func TestSingBoxCustomTemplatePreservesSectionsAndExpandsGroups(t *testing.T) {
	template := []byte(`{
  "$schema":"https://sing-box.sagernet.org/schema.json",
  "log":{"level":"error"}, "dns":{"servers":[{"type":"udp","tag":"local","server":"9.9.9.9"}],"final":"local"},
  "ntp":{"enabled":false}, "certificate":{"store":"system"}, "certificate_providers":[],
  "http_clients":[], "network_namespaces":[], "endpoints":[], "services":[],
  "inbounds":[{"type":"mixed","tag":"custom-in","listen":"127.0.0.1","listen_port":1089}],
  "outbounds":[{"type":"selector","tag":"pick","outbounds":["fast","direct","{{nodes}}"],"default":"fast"},
    {"type":"urltest","tag":"fast","outbounds":["{{nodes}}"],"url":"https://example.com/test","interval":"5m","tolerance":70},
    {"type":"direct","tag":"direct"}],
  "route":{"final":"pick","rules":[{"domain_suffix":["example.com"],"action":"route","outbound":"direct"}]},
  "experimental":{"clash_api":{"external_controller":"127.0.0.1:9191","secret":"private-controller-secret"}},
  "future_setting":{"integer":9007199254740993}
}`)
	nodes := []string{testVLESS("type=tcp#pick"), testVLESS("type=tcp#node-two")}
	body, report := BuildSingBoxConfigWithTemplate(nodes, SingBoxOptions{Mode: "invalid", MixedPort: -1}, template)
	if report.Status != "ready" || report.Converted != 2 {
		t.Fatalf("report = %+v", report)
	}
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal(template, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatal(err)
	}
	for key, original := range before {
		if key == "outbounds" {
			continue
		}
		var left, right bytes.Buffer
		if err := json.Compact(&left, original); err != nil {
			t.Fatal(err)
		}
		if err := json.Compact(&right, after[key]); err != nil {
			t.Fatal(err)
		}
		var a, b any
		dec := json.NewDecoder(&left)
		dec.UseNumber()
		_ = dec.Decode(&a)
		dec = json.NewDecoder(&right)
		dec.UseNumber()
		_ = dec.Decode(&b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("template section changed: %s", key)
		}
	}
	var outbounds []singBoxOutbound
	if err := json.Unmarshal(after["outbounds"], &outbounds); err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 5 || outbounds[3].Tag == "pick" || outbounds[4].Tag != "node-two" {
		t.Fatal("nodes missing or static tag collision not handled")
	}
	if !reflect.DeepEqual(outbounds[1].Outbounds, []string{outbounds[3].Tag, "node-two"}) || len(outbounds[0].Outbounds) != 4 {
		t.Fatal("node groups were not populated")
	}
	if strings.Contains(string(body), "{{nodes}}") {
		t.Fatal("placeholder leaked into output")
	}
}

func TestSingBoxTemplateValidationAndEmptyGroups(t *testing.T) {
	node := []string{testVLESS("type=tcp#test")}
	for _, raw := range []string{
		`null`, `[]`, `{} {}`, `{"dns":{},"dns":{}}`, `{"dns":{"final":"one","final":"two"}}`,
		`{"inbounds":{}}`, `{"dns":[]}`, `{"outbounds":["{{nodes}}"]}`, `{"outbounds":[{"type":"direct"}]}`,
		`{"outbounds":[{"type":"direct","tag":"a"},{"type":"direct","tag":"a"}]}`,
		`{"outbounds":[{"type":"selector","tag":"a","outbounds":"test"}]}`,
		`{"outbounds":[{"type":"selector","tag":"a","outbounds":["missing"]}]}`,
		`{"outbounds":[{"type":"selector","tag":"a","outbounds":["test"],"default":"wrong"}]}`,
		`{"outbounds":[{"type":"selector","tag":"a","outbounds":["b"]},{"type":"selector","tag":"b","outbounds":["a"]}]}`,
		`{"route":{"final":"missing"}}`, strings.Repeat("x", maxSingBoxTemplateBytes+1),
		`{"dns":{"servers":[{"type":"udp","server":"223.5.5.5","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`,
	} {
		body, report := BuildSingBoxConfigWithTemplate(node, SingBoxOptions{}, []byte(raw))
		if len(body) != 0 || len(report.Diagnostics) == 0 || report.Diagnostics[0].Code != "INVALID_TEMPLATE" {
			t.Fatal("invalid template was accepted")
		}
	}
	for _, members := range []string{`[]`, `["{{nodes}}"]`} {
		body, report := BuildSingBoxConfigWithTemplate(node, SingBoxOptions{}, []byte(`{"outbounds":[{"type":"selector","tag":"a","outbounds":`+members+`}],"route":{"final":"a"}}`))
		if len(body) == 0 || report.Status != "ready" {
			t.Fatal("empty group did not receive generated nodes")
		}
	}
	if body, report := BuildSingBoxConfigWithTemplate(node, SingBoxOptions{}, []byte(`{}`)); len(body) == 0 || report.Status != "ready" {
		t.Fatal("nodes were not appended to a template without outbounds")
	}
}

func TestSingBoxTemplateDatabasePersistenceAndIsolation(t *testing.T) {
	store := newTestStore(t)
	initial, err := store.SingBoxTemplate()
	if err != nil || !bytes.Contains(initial, []byte("{{nodes}}")) {
		t.Fatal("local preset was not initialized")
	}
	var stored model.SingBoxTemplate
	if err := store.db.First(&stored, subscriptionRecordID).Error; err != nil || stored.Content != string(initial) {
		t.Fatal("preset snapshot differed from SQLite")
	}
	if err := store.SaveSingBoxTemplate([]byte(`{"outbounds":[{"type":"selector","tag":"a","outbounds":["b"]},{"type":"selector","tag":"b","outbounds":["a"]}]}`)); !IsSingBoxTemplateError(err) {
		t.Fatal("cyclic template accepted before nodes were added")
	}
	if err := store.SaveSingBoxTemplate(initial); err != nil {
		t.Fatal("valid preset cannot be saved without nodes", err)
	}
	if err := store.SaveSingBoxTemplate([]byte(`{"future":[` + strings.Repeat("0,", 80000) + `0]}`)); !IsSingBoxTemplateError(err) {
		t.Fatal("template exceeding the limit after formatting was saved")
	}
	if err := store.ReplaceNodes([]string{testVLESS("type=tcp#one")}); err != nil {
		t.Fatal(err)
	}
	_, oldReport, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	custom := []byte(`{"log":{"level":"error"},"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":1087}],"outbounds":[{"type":"selector","tag":"custom","outbounds":["{{nodes}}"]}],"route":{"final":"custom"}}`)
	if err := store.SaveSingBoxTemplate(custom); err != nil {
		t.Fatal(err)
	}
	body, report, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || len(body) == 0 || report.SourceHash == oldReport.SourceHash || report.TemplateHash == oldReport.TemplateHash {
		t.Fatal("template did not invalidate publication cache")
	}
	saved, _ := store.SingBoxTemplate()
	if err := store.SaveSingBoxTemplate([]byte(`{"outbounds":`)); err == nil {
		t.Fatal("invalid template saved")
	}
	current, _ := store.SingBoxTemplate()
	if !bytes.Equal(saved, current) {
		t.Fatal("invalid save destroyed the previous template")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(store.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	loaded, err := reopened.SingBoxTemplate()
	if err != nil || !bytes.Equal(loaded, saved) {
		t.Fatal("template did not survive restart")
	}
	if err := reopened.ReplaceNodes([]string{testVLESS("type=tcp#one"), "unknown://invalid"}); err != nil {
		t.Fatal(err)
	}
	if result, state, err := reopened.SingBoxSubscription(SingBoxOptions{}); err != nil || state.Status != "stale" || !bytes.Equal(result, body) {
		t.Fatal("unchanged template lost last-good publication")
	}
	if err := reopened.SaveSingBoxTemplate([]byte(`{"log":{"level":"debug"}}`)); err != nil {
		t.Fatal(err)
	}
	if result, state, err := reopened.SingBoxSubscription(SingBoxOptions{}); err != nil || len(result) != 0 || state.Status != "error" {
		t.Fatal("changed template exposed old DNS/routing as a fallback")
	}
}

func TestSingBoxTemplateConcurrentSavesAndExports(t *testing.T) {
	store := newTestStore(t)
	if err := store.ReplaceNodes([]string{testVLESS("type=tcp")}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 6; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			preset, err := DefaultSingBoxTemplate(SingBoxOptions{Mode: []string{"tun", "mixed"}[index%2]})
			if err != nil {
				t.Error(err)
				return
			}
			if err := store.SaveSingBoxTemplate(preset); err != nil {
				t.Error(err)
			}
			if _, _, err := store.SingBoxSubscription(SingBoxOptions{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	workers.Wait()
	template, err := store.SingBoxTemplate()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := BuildSingBoxConfigWithTemplate([]string{testVLESS("type=tcp")}, SingBoxOptions{}, template)
	got, _, err := store.SingBoxSubscription(SingBoxOptions{})
	if err != nil || !bytes.Equal(want, got) {
		t.Fatal("concurrent export was not generated from current template")
	}
}
