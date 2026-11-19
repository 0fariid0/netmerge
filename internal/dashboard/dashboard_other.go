//go:build !windows

package dashboard

import "errors"

func Open(string) (*Session, error) { return nil, errors.New("dashboard is available on Windows only") }
func Run(string) error              { return errors.New("dashboard is available on Windows only") }
