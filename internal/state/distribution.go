package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"net-merge/internal/engine"
)

func LoadDistribution(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(limitsDir(dir), "distribution"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(string(data))
	if !engine.ValidDistribution(mode) {
		return "", fmt.Errorf("invalid saved distribution mode %q", mode)
	}
	return mode, nil
}

func SaveDistribution(dir, mode string) error {
	if !engine.ValidDistribution(mode) {
		return fmt.Errorf("invalid distribution mode %q", mode)
	}
	dir = limitsDir(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".distribution-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(mode + "\n")
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return replaceFile(tmp.Name(), filepath.Join(dir, "distribution"))
}
