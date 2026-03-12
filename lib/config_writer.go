package lib

import (
	"fmt"
	"os"
)

func PersistConfigToFile() error {
	path := getConfigPath()
	if path == "" {
		return fmt.Errorf("PersistConfigToFile: config path not set, call lib.SetConfigPath first")
	}

	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("PersistConfigToFile: failed to create temp file: %w", err)
	}

	if err := PersistConfig(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("PersistConfigToFile: failed to persist config: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("PersistConfigToFile: failed to close temp file: %w", err)
	}

	return os.Rename(tmp, path)
}
