package main

import (
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"git.local/gosub/controller"
	"git.local/gosub/lib"
)

//go:embed assets/app.css assets/app.js template/*.html
var appFS embed.FS

const (
	configPath = "config.json"
	dbPath     = "gosub.db"
)

func NewDefaultConfig() *lib.Config {
	randomStr, err := lib.RandomString(16)
	if err != nil {
		log.Fatal(err)
	}
	password, err := lib.RandomString(32)
	if err != nil {
		log.Fatal(err)
	}
	return &lib.Config{
		Settings: &lib.Settings{
			Host:     "127.0.0.1",
			Port:     8080,
			Admin:    fmt.Sprintf("admin_%s", randomStr),
			Password: password,
			TLS:      &lib.TLSConfig{Enabled: false},
		},
	}
}

func WriteDefaultConfig(cfg *lib.Config) error {
	encoded, err := lib.EncodeConfig(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, encoded, 0600)
}

func InitializeRuntime() (*lib.Settings, string, bool) {
	// 设置配置路径还有sqlite数据库路径
	lib.SetConfigPath(configPath)

	configCreated, err := ensureConfigFile()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("初始化 sqlite data")
	if err := lib.InitializeStore(dbPath); err != nil {
		log.Fatal(err)
	}
	// 获取配置
	settings, err := lib.GetSettings()
	if err != nil {
		log.Fatal(err)
	}
	if store, err := lib.DefaultStore(); err == nil {
		if _, err := store.SingBoxTemplate(); err != nil {
			log.Printf("sing-box 数据库模板初始化失败: %v", err)
		}
	}
	// 获取订阅url路径
	path, err := lib.GetSubscriptionPath()
	if err != nil {
		log.Fatal(err)
	}
	return settings, path, configCreated
}

func ensureConfigFile() (bool, error) {
	if err := validateConfigFile(); errors.Is(err, os.ErrNotExist) {
		if err := WriteDefaultConfig(NewDefaultConfig()); err != nil {
			return false, fmt.Errorf("failed to write default config: %w", err)
		}
		if err := validateConfigFile(); err != nil {
			return false, err
		}
		return true, restrictConfigFileMode()
	} else if err != nil {
		return false, err
	}
	return false, restrictConfigFileMode()
}

func validateConfigFile() error {
	f, err := os.Open(configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = lib.ParseConfig(f)
	return err
}

func restrictConfigFileMode() error {
	if err := os.Chmod(configPath, 0600); err != nil {
		return fmt.Errorf("set private config permissions: %w", err)
	}
	return nil
}

func main() {
	// 初始化
	settings, path, configCreated := InitializeRuntime()
	// 创建gin服务
	server := controller.NewServer(appFS)
	printStartupInfo(settings, path, configCreated)

	addr := net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port))
	if settings.TLS != nil && settings.TLS.Enabled {
		log.Fatal(server.RunTLSWithSamePortRedirect(addr, settings.TLS.CertFile, settings.TLS.KeyFile))
	}
	log.Fatal(server.Run(addr))
}

func printStartupInfo(settings *lib.Settings, path string, configCreated bool) {
	webURL, _ := startupURLs(settings, path)
	tlsEnabled := settings.TLS != nil && settings.TLS.Enabled

	log.Println("GoSub 启动")
	log.Printf("Web 地址=%s", webURL)
	log.Print("订阅地址请登录管理后台查看")
	log.Printf("监听地址=%s", net.JoinHostPort(settings.Host, fmt.Sprintf("%d", settings.Port)))
	log.Printf("管理员=%s", settings.Admin)
	if configCreated {
		log.Print("初始账号与密码已写入 config.json，请在本机查看")
	} else {
		log.Print("密码=(hidden)")
	}
	log.Printf("TLS 启用=%t", tlsEnabled)
	if tlsEnabled {
		log.Printf("TLS 证书文件=%s", settings.TLS.CertFile)
		log.Printf("TLS 密钥文件=%s", settings.TLS.KeyFile)
	}
}

func startupURLs(settings *lib.Settings, path string) (string, string) {
	scheme := "http"
	if settings.TLS != nil && settings.TLS.Enabled {
		scheme = "https"
	}
	host := settings.Host
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := scheme + "://" + net.JoinHostPort(host, strconv.Itoa(settings.Port))
	return base + "/", base + path
}
