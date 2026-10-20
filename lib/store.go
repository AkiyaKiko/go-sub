package lib

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.local/gosub/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const subscriptionRecordID uint = 1
const DefaultRouteRuleJSON = "{\n  \"rules\": []\n}"
const maxRouteRuleJSONBytes = 512 * 1024

var (
	defaultStoreMu sync.RWMutex
	defaultStore   *Store
)

type Store struct {
	db        *gorm.DB
	singBoxMu sync.Mutex
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

type RouteRuleSnapshot struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func InitializeStore(dbPath string) error {
	store, err := OpenStore(dbPath)
	if err != nil {
		return err
	}
	if _, _, err := store.EnsureSubscriptionPath(); err != nil {
		return err
	}
	SetDefaultStore(store)
	return nil
}

func OpenStore(dbPath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sqlite database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&model.Subscription{}, &model.Node{}, &model.RouteRule{}, &model.SingBoxPublication{}); err != nil {
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}
	if err := restrictFileMode(dbPath); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func SetDefaultStore(store *Store) {
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()
	defaultStore = store
}

func DefaultStore() (*Store, error) {
	defaultStoreMu.RLock()
	defer defaultStoreMu.RUnlock()
	if defaultStore == nil {
		return nil, fmt.Errorf("数据库还没有初始化")
	}
	return defaultStore, nil
}

func (s *Store) Nodes() ([]string, error) {
	var records []model.Node
	if err := s.db.Order("position ASC, id ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	nodes := make([]string, 0, len(records))
	for _, record := range records {
		nodes = append(nodes, record.Value)
	}
	return nodes, nil
}

func (s *Store) ReplaceNodes(nodes []string) error {
	s.singBoxMu.Lock()
	defer s.singBoxMu.Unlock()
	nodes = NormalizeNodes(nodes)
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&model.Node{}).Error; err != nil {
			return fmt.Errorf("delete old nodes: %w", err)
		}
		for i, value := range nodes {
			node := &model.Node{Value: value, Position: i}
			if err := tx.Create(node).Error; err != nil {
				return fmt.Errorf("save node %d: %w", i+1, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// A failed sing-box export must never roll back the existing Xray node save.
	_, _, _ = s.syncSingBoxLocked(currentSingBoxOptions())
	return nil
}

func currentSingBoxOptions() SingBoxOptions {
	if settings, err := GetSettings(); err == nil && settings.SingBox != nil {
		return *settings.SingBox
	}
	return SingBoxOptions{}
}

func (s *Store) SingBoxSubscription(options SingBoxOptions) ([]byte, ConversionReport, error) {
	s.singBoxMu.Lock()
	defer s.singBoxMu.Unlock()
	return s.syncSingBoxLocked(options)
}

func (s *Store) syncSingBoxLocked(options SingBoxOptions) ([]byte, ConversionReport, error) {
	nodes, err := s.Nodes()
	if err != nil {
		return nil, ConversionReport{}, err
	}
	if normalized, optionErr := NormalizeSingBoxOptions(options); optionErr == nil {
		options = normalized
	}
	source, err := json.Marshal(struct {
		Nodes    []string
		Options  SingBoxOptions
		Template string
	}{nodes, options, SingBoxTemplateVersion})
	if err != nil {
		return nil, ConversionReport{}, err
	}
	sourceHash := fmt.Sprintf("%x", sha256.Sum256(source))
	publication := model.SingBoxPublication{ID: subscriptionRecordID}
	if err := s.db.First(&publication, subscriptionRecordID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ConversionReport{}, fmt.Errorf("load sing-box publication: %w", err)
	}
	var report ConversionReport
	if publication.SourceHash == sourceHash && json.Unmarshal([]byte(publication.ReportJSON), &report) == nil {
		return []byte(publication.Body), report, nil
	}
	body, report := BuildSingBoxConfig(nodes, options)
	report.SourceHash = sourceHash
	if len(body) > 0 {
		publication.Body = string(body)
		publication.PublishedHash = sourceHash
		publication.GeneratedAt = time.Now().UTC()
	} else if report.Status == "disabled" {
		publication.Body, publication.PublishedHash = "", ""
		publication.GeneratedAt = time.Time{}
	} else if publication.Body != "" {
		var previous struct {
			Inbounds []struct {
				Type string `json:"type"`
			} `json:"inbounds"`
		}
		mode := options.Mode
		if mode == "" {
			mode = "tun"
		}
		if json.Unmarshal([]byte(publication.Body), &previous) == nil && len(previous.Inbounds) == 1 && previous.Inbounds[0].Type == mode {
			report.Status = "stale"
		} else {
			// An old Mixed profile cannot satisfy a request for a VPN profile (or vice versa).
			publication.Body, publication.PublishedHash = "", ""
			publication.GeneratedAt = time.Time{}
		}
	}
	publication.SourceHash = sourceHash
	report.PublishedHash = publication.PublishedHash
	if !publication.GeneratedAt.IsZero() {
		report.GeneratedAt = publication.GeneratedAt.Format(time.RFC3339)
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return nil, report, err
	}
	publication.ReportJSON = string(reportJSON)
	if err := s.db.Save(&publication).Error; err != nil {
		return nil, report, fmt.Errorf("save sing-box publication: %w", err)
	}
	return []byte(publication.Body), report, nil
}

func GetSingBoxSubscription() ([]byte, ConversionReport, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, ConversionReport{}, err
	}
	return store.SingBoxSubscription(currentSingBoxOptions())
}

func (s *Store) EnsureSubscriptionPath() (string, bool, error) {
	path, err := s.SubscriptionPath()
	if err == nil {
		return path, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, err
	}

	if path, ok := s.legacySubscriptionPath(); ok {
		if err := s.UpdateSubscriptionPath(path); err != nil {
			return "", false, err
		}
		return path, true, nil
	}

	path, err = RandomPublicPath()
	if err != nil {
		return "", false, err
	}
	if err := s.UpdateSubscriptionPath(path); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func (s *Store) legacySubscriptionPath() (string, bool) {
	if !s.db.Migrator().HasTable("settings") {
		return "", false
	}

	var legacy struct {
		Path string
	}
	if err := s.db.Table("settings").Select("path").Where("id = ?", 1).Take(&legacy).Error; err != nil {
		return "", false
	}
	if err := ValidateSubscriptionPath(legacy.Path); err != nil {
		return "", false
	}
	return legacy.Path, true
}

func (s *Store) SubscriptionPath() (string, error) {
	var record model.Subscription
	if err := s.db.First(&record, subscriptionRecordID).Error; err != nil {
		return "", err
	}
	return record.Path, nil
}

func (s *Store) UpdateSubscriptionPath(path string) error {
	path = strings.TrimSpace(path)
	if err := ValidateSubscriptionPath(path); err != nil {
		return err
	}
	if err := s.validatePathNotUsedByRouteRule(path, 0); err != nil {
		return err
	}
	if err := s.validatePathNotUsedByRouteRule(SingBoxPath(path), 0); err != nil {
		return err
	}
	record := &model.Subscription{ID: subscriptionRecordID, Path: path}
	if err := s.db.Save(record).Error; err != nil {
		return fmt.Errorf("save subscription path: %w", err)
	}
	return nil
}

func (s *Store) RouteRules() ([]RouteRuleSnapshot, error) {
	var records []model.RouteRule
	if err := s.db.Order("id ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load route rules: %w", err)
	}
	rules := make([]RouteRuleSnapshot, 0, len(records))
	for _, record := range records {
		rules = append(rules, routeRuleSnapshot(record))
	}
	return rules, nil
}

func (s *Store) CreateRouteRule(name, path, content string) error {
	name = NormalizeRouteRuleName(name)
	path = strings.TrimSpace(path)
	if err := ValidateSubscriptionPath(path); err != nil {
		return err
	}
	if err := s.validateRouteRulePath(path, 0); err != nil {
		return err
	}
	normalizedContent, err := NormalizeJSONText(content)
	if err != nil {
		return err
	}
	record := &model.RouteRule{Name: name, Path: path, Content: normalizedContent}
	if err := s.db.Create(record).Error; err != nil {
		return fmt.Errorf("save route rule: %w", err)
	}
	return nil
}

func (s *Store) UpdateRouteRule(id uint, name, path, content string) error {
	name = NormalizeRouteRuleName(name)
	path = strings.TrimSpace(path)
	if err := ValidateSubscriptionPath(path); err != nil {
		return err
	}
	if err := s.validateRouteRulePath(path, id); err != nil {
		return err
	}
	normalizedContent, err := NormalizeJSONText(content)
	if err != nil {
		return err
	}

	var record model.RouteRule
	if err := s.db.First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("route rule not found")
		}
		return fmt.Errorf("load route rule: %w", err)
	}
	record.Name = name
	record.Path = path
	record.Content = normalizedContent
	if err := s.db.Save(&record).Error; err != nil {
		return fmt.Errorf("save route rule: %w", err)
	}
	return nil
}

func (s *Store) DeleteRouteRule(id uint) error {
	result := s.db.Delete(&model.RouteRule{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete route rule: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("route rule not found")
	}
	return nil
}

func (s *Store) RouteRuleByPath(path string) (RouteRuleSnapshot, bool, error) {
	var record model.RouteRule
	err := s.db.Where("path = ?", strings.TrimSpace(path)).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RouteRuleSnapshot{}, false, nil
	}
	if err != nil {
		return RouteRuleSnapshot{}, false, fmt.Errorf("load route rule: %w", err)
	}
	return routeRuleSnapshot(record), true, nil
}

func (s *Store) RandomRouteRulePath() (string, error) {
	for range 16 {
		path, err := RandomPublicPath()
		if err != nil {
			return "", err
		}
		if err := s.validateRouteRulePath(path, 0); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique route rule path")
}

func (s *Store) validateRouteRulePath(path string, excludeRouteRuleID uint) error {
	subscriptionPath, err := s.SubscriptionPath()
	if err == nil && (subscriptionPath == path || SingBoxPath(subscriptionPath) == path) {
		return fmt.Errorf("path already used by subscription")
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return s.validatePathNotUsedByRouteRule(path, excludeRouteRuleID)
}

func (s *Store) validatePathNotUsedByRouteRule(path string, excludeRouteRuleID uint) error {
	var count int64
	query := s.db.Model(&model.RouteRule{}).Where("path = ?", path)
	if excludeRouteRuleID != 0 {
		query = query.Where("id <> ?", excludeRouteRuleID)
	}
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check route rule path: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("path already used by route rule")
	}
	return nil
}

func routeRuleSnapshot(record model.RouteRule) RouteRuleSnapshot {
	return RouteRuleSnapshot{
		ID:      record.ID,
		Name:    record.Name,
		Path:    record.Path,
		Content: record.Content,
	}
}

func GetSettings() (*Settings, error) {
	cfg, err := LoadConfigFile()
	if err != nil {
		return nil, err
	}
	return CloneSettings(cfg.Settings), nil
}

func GetNodes() ([]string, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return store.Nodes()
}

func GetConfigSnapshot() (*Config, error) {
	settings, err := GetSettings()
	if err != nil {
		return nil, err
	}
	nodes, err := GetNodes()
	if err != nil {
		return nil, err
	}
	return &Config{Settings: settings, Nodes: nodes}, nil
}

func GetRouteRules() ([]RouteRuleSnapshot, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return store.RouteRules()
}

func GetSubscriptionPath() (string, error) {
	store, err := DefaultStore()
	if err != nil {
		return "", err
	}
	return store.SubscriptionPath()
}

func GetSubscriptionURL() (string, error) {
	settings, err := GetSettings()
	if err != nil {
		return "", err
	}
	path, err := GetSubscriptionPath()
	if err != nil {
		return "", err
	}
	scheme := "http"
	if settings.TLS != nil && settings.TLS.Enabled {
		scheme = "https"
	}
	host := settings.Host
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(settings.Port)) + path, nil
}

func UpdateSubscriptionPath(path string) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.UpdateSubscriptionPath(path)
}

func CreateRouteRule(name, path, content string) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.CreateRouteRule(name, path, content)
}

func UpdateRouteRule(id uint, name, path, content string) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.UpdateRouteRule(id, name, path, content)
}

func DeleteRouteRule(id uint) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.DeleteRouteRule(id)
}

func GetRouteRuleByPath(path string) (RouteRuleSnapshot, bool, error) {
	store, err := DefaultStore()
	if err != nil {
		return RouteRuleSnapshot{}, false, err
	}
	return store.RouteRuleByPath(path)
}

func RandomRouteRulePath() (string, error) {
	store, err := DefaultStore()
	if err != nil {
		return "", err
	}
	return store.RandomRouteRulePath()
}

func UpdateNodes(nodes []string) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.ReplaceNodes(nodes)
}

func ChangeNodes(newNodes []string) bool {
	return UpdateNodes(newNodes) == nil
}

func NormalizeRouteRuleName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "路由规则"
	}
	return name
}

func NormalizeJSONText(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultRouteRuleJSON
	}
	if len([]byte(raw)) > maxRouteRuleJSONBytes {
		return "", fmt.Errorf("route rule json is too large")
	}
	if !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("route rule json is invalid")
	}

	var out bytes.Buffer
	if err := json.Indent(&out, []byte(raw), "", "  "); err != nil {
		return "", fmt.Errorf("format route rule json: %w", err)
	}
	return out.String(), nil
}

func restrictFileMode(path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}
	if err := os.Chmod(path, 0600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("set private file permissions for %s: %w", path, err)
	}
	return nil
}
