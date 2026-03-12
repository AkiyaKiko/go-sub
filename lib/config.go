package lib

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

var GLOBAL_CONFIG Config

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`
}

type Settings struct {
	Host     string     `json:"host"`
	Port     int        `json:"port"`
	Path     string     `json:"path"`
	Admin    string     `json:"admin"`
	Password string     `json:"password"`
	TLS      *TLSConfig `json:"tls,omitempty"` // Optional
}

// 内部状态：包含锁（不要把这个类型按值传递给 JSON）
type Config struct {
	mutex    sync.RWMutex `json:"-"`
	Settings *Settings    `json:"settings"`
	Nodes    []string     `json:"nodes,omitempty"` // Optional
}

// 对外 / 持久化 / JSON 的 DTO：不包含锁
type ConfigDTO struct {
	Settings *Settings `json:"settings"`
	Nodes    []string  `json:"nodes,omitempty"`
}

// SnapshotDTO：在锁内拷贝出稳定快照，锁外做 JSON
func (c *Config) SnapshotDTO() ConfigDTO {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var sCopy *Settings
	if c.Settings != nil {
		tmp := *c.Settings // 拷贝 Settings 值
		if c.Settings.TLS != nil {
			t := *c.Settings.TLS // 拷贝 TLSConfig
			tmp.TLS = &t
		}
		sCopy = &tmp
	}

	var nodesCopy []string
	if c.Nodes != nil {
		nodesCopy = append([]string(nil), c.Nodes...) // 拷贝 slice
	}

	return ConfigDTO{
		Settings: sCopy,
		Nodes:    nodesCopy,
	}
}

func ParseConfig(r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var cfg ConfigDTO
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	if cfg.Settings == nil {
		return fmt.Errorf(`config error: "settings" field is required!`)
	}
	if cfg.Settings.Port == 0 || cfg.Settings.Path == "" {
		return fmt.Errorf(`config error: "settings.port" and "settings.path" are required!`)
	}

	// host 默认值
	if cfg.Settings.Host == "" {
		cfg.Settings.Host = "127.0.0.1"
	}

	// host 白名单校验
	switch cfg.Settings.Host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::", "::1":
		// ok
	default:
		return fmt.Errorf(
			`config error: invalid "settings.host"=%q, allowed: localhost, 127.0.0.1, 0.0.0.0 (optional: ::, ::1)`,
			cfg.Settings.Host,
		)
	}

	if cfg.Settings.TLS == nil {
		cfg.Settings.TLS = &TLSConfig{Enabled: false}
	}

	if !cfg.Settings.TLS.Enabled {
		cfg.Settings.TLS.CertFile = ""
		cfg.Settings.TLS.KeyFile = ""
	} else {
		if cfg.Settings.TLS.CertFile == "" || cfg.Settings.TLS.KeyFile == "" {
			return fmt.Errorf(`config error: when TLS is enabled, "settings.tls.certFile" and "settings.tls.keyFile" are required!`)
		}
	}

	// 写锁：原子更新全局配置
	GLOBAL_CONFIG.mutex.Lock()
	defer GLOBAL_CONFIG.mutex.Unlock()

	// 这里可以直接引用 cfg.Settings（如果你担心外部仍持有并修改，可做一次深拷贝）
	GLOBAL_CONFIG.Settings = cfg.Settings
	GLOBAL_CONFIG.Nodes = append([]string(nil), cfg.Nodes...) // 拷贝一份更稳妥
	return nil
}

func PersistConfig(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	snap := GLOBAL_CONFIG.SnapshotDTO()
	return enc.Encode(snap)
}

func GetSettings() (*Settings, error) {
	GLOBAL_CONFIG.mutex.RLock()
	defer GLOBAL_CONFIG.mutex.RUnlock()

	if GLOBAL_CONFIG.Settings == nil {
		return nil, fmt.Errorf(`[GetSettings] config not initialized: call ParseConfig first`)
	}

	tmp := *GLOBAL_CONFIG.Settings
	if GLOBAL_CONFIG.Settings.TLS != nil {
		t := *GLOBAL_CONFIG.Settings.TLS
		tmp.TLS = &t
	}
	return &tmp, nil
}

func GetNodes() ([]string, error) {
	GLOBAL_CONFIG.mutex.RLock()
	defer GLOBAL_CONFIG.mutex.RUnlock()

	if GLOBAL_CONFIG.Nodes == nil {
		return nil, fmt.Errorf(`[GetNodes] cannot get nodes: config not initialized or "nodes" field is missing`)
	}

	return append([]string(nil), GLOBAL_CONFIG.Nodes...), nil
}

func ChangeSubUri(newUri string) (ok bool) {
	GLOBAL_CONFIG.mutex.Lock()
	defer GLOBAL_CONFIG.mutex.Unlock()

	if GLOBAL_CONFIG.Settings == nil {
		return false
	}
	GLOBAL_CONFIG.Settings.Path = newUri
	return true
}

func ChangeNodes(newNodes []string) (ok bool) {
	GLOBAL_CONFIG.mutex.Lock()
	defer GLOBAL_CONFIG.mutex.Unlock()

	GLOBAL_CONFIG.Nodes = append([]string(nil), newNodes...) // 拷贝，避免调用方后续修改影响内部
	return true
}

// Outerside Call
func EncodeConfig(r_cfg *Config) ([]byte, error) {
	if r_cfg == nil {
		return nil, fmt.Errorf("EncodeConfig: nil config")
	}
	snap := r_cfg.SnapshotDTO()
	return json.MarshalIndent(snap, "", "  ")
}
