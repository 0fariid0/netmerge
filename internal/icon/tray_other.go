//go:build !windows

package icon

import _ "embed"

//go:embed net-merge.png
var trayPNG []byte

// Tray returns the icon in the non-Windows tray library's format.
func Tray() []byte { return trayPNG }
