// Package dashboard connects the native Windows window to the running tray.
package dashboard

import "net-merge/internal/engine"

// Snapshot is a read-only view of the running tray. It includes adapters while
// merging is paused, so the window still shows physical link activity.
type Snapshot struct {
	AutoPause  bool
	V6Auto     bool
	SignIn     bool
	On         bool
	Waiting    bool
	RoundRobin bool
	Error      string
	Adapters   []engine.SlotStats
}

type Command string

const (
	Start      Command = "start"
	Pause      Command = "pause"
	Smart      Command = "smart"
	RoundRobin Command = "round-robin"
	Releases   Command = "releases"
)

// Session owns the pipes to one dashboard window.
type Session struct {
	snapshots chan Snapshot
	stop      chan struct{}
	Commands  <-chan Command
	Done      <-chan error
}

// Update offers the latest state without blocking tray controls behind the UI.
func (s *Session) Update(snapshot Snapshot) {
	if len(s.snapshots) != 0 {
		select {
		case <-s.snapshots:
		default:
		}
	}
	select {
	case s.snapshots <- snapshot:
	default:
	}
}

// Close ends the feed. The child closes its window when it loses the feed.
func (s *Session) Close() { close(s.stop); close(s.snapshots) }
