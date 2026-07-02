package lib

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const subscriptionRecordID uint = 1

var (
	defaultStoreMu sync.RWMutex
	defaultStore   *Store
)

type Store struct {
	db *gorm.DB
}

type nodeRecord struct {
	ID        uint `gorm:"primaryKey"`
	Value     string
	Position  int `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (nodeRecord) TableName() string {
	return "nodes"
}

type subscriptionRecord struct {
	ID        uint `gorm:"primaryKey"`
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (subscriptionRecord) TableName() string {
	return "subscription"
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

	if err := db.AutoMigrate(&subscriptionRecord{}, &nodeRecord{}); err != nil {
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
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
		return nil, fmt.Errorf("store not initialized")
	}
	return defaultStore, nil
}

func (s *Store) Nodes() ([]string, error) {
	var records []nodeRecord
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
	nodes = NormalizeNodes(nodes)
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&nodeRecord{}).Error; err != nil {
			return fmt.Errorf("delete old nodes: %w", err)
		}
		for i, value := range nodes {
			node := &nodeRecord{Value: value, Position: i}
			if err := tx.Create(node).Error; err != nil {
				return fmt.Errorf("save node %d: %w", i+1, err)
			}
		}
		return nil
	})
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

	random, err := RandomString(8)
	if err != nil {
		return "", false, err
	}
	path = "/" + random
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
	var record subscriptionRecord
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
	record := &subscriptionRecord{ID: subscriptionRecordID, Path: path}
	if err := s.db.Save(record).Error; err != nil {
		return fmt.Errorf("save subscription path: %w", err)
	}
	return nil
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
