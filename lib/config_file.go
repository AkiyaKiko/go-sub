package lib

import (
	"fmt"
	"os"
	"sync"
)

var (
	configFileMu   sync.RWMutex
	configFilePath string
)

func SetConfigPath(path string) {
	configFileMu.Lock()
	defer configFileMu.Unlock()
	configFilePath = path
}

func getConfigPath() string {
	configFileMu.RLock()
	defer configFileMu.RUnlock()
	return configFilePath
}

func LoadConfigFile() (*Config, error) {
	path := getConfigPath()
	if path == "" {
		return nil, fmt.Errorf("config path not set")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseConfig(f)
}
