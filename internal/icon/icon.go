// Package icon serves the NetMerge brand artwork.
package icon

import _ "embed"

//go:embed net-merge.ico
var windowsIcon []byte

// ICO returns the multi-resolution Windows application icon.
func ICO() []byte { return windowsIcon }
