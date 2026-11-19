package dashboard

import (
	"encoding/json"
	"os"
	"os/exec"
	"syscall"

	"net-merge/internal/logx"
)

// Open runs the GUI in a child with its own Windows message loop. Dedicated
// pipes carry snapshots and a small set of commands with no local listener.
func Open(version string) (*Session, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "_dashboard", version)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}

	snapshots := make(chan Snapshot, 1)
	commands := make(chan Command)
	done := make(chan struct{})
	go func() {
		defer input.Close()
		encoder := json.NewEncoder(input)
		for snapshot := range snapshots {
			if err := encoder.Encode(snapshot); err != nil {
				return
			}
		}
	}()
	go func() {
		decoder := json.NewDecoder(output)
		for {
			var action Command
			if err := decoder.Decode(&action); err != nil {
				break
			}
			commands <- action
		}
		if err := cmd.Wait(); err != nil {
			logx.Printf("dashboard: %v\n", err)
		}
		close(done)
	}()
	return &Session{snapshots: snapshots, Commands: commands, Done: done}, nil
}
