package lib

import "sync"

var (
	configPathMu sync.RWMutex
	configPath   string
)

func SetConfigPath(path string) {
	configPathMu.Lock()
	defer configPathMu.Unlock()
	configPath = path
}

func getConfigPath() string {
	configPathMu.RLock()
	defer configPathMu.RUnlock()
	return configPath
}
