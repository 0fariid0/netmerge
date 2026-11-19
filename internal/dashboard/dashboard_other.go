//go:build !windows

package dashboard

import "errors"

func Open(string, string, bool) (*Session, error) {
	return nil, errors.New("dashboard is available on Windows only")
}

func Smoke() error { return errors.New("dashboard is available on Windows only") }
