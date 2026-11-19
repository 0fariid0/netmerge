package dashboard

import (
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"net-merge/internal/engine"
	"net-merge/internal/logx"
)

//go:embed window.ps1
var windowScript []byte

// Open runs the GUI in a child with its own Windows message loop. Dedicated
// pipes carry snapshots and a small set of commands with no local listener.
func Open(version, language string, popup bool) (*Session, error) {
	root, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	exe := filepath.Join(root, "WindowsPowerShell", "v1.0", "powershell.exe")
	const bootstrap = `$ErrorActionPreference = "Stop"; $reader = [IO.StreamReader]::new([Console]::OpenStandardInput(), [Text.Encoding]::UTF8); $source = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($reader.ReadLine())); & ([ScriptBlock]::Create($source))`
	command := utf16.Encode([]rune(bootstrap))
	encoded := make([]byte, len(command)*2)
	for i, unit := range command {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	cmd := exec.Command(exe, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.Env = append(os.Environ(), "NETMERGE_UI_VERSION="+version, "NETMERGE_UI_LANGUAGE="+language, fmt.Sprintf("NETMERGE_UI_POPUP=%t", popup))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}

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
	done := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		defer input.Close()
		if _, err := fmt.Fprintln(input, base64.StdEncoding.EncodeToString(windowScript)); err != nil {
			return
		}
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
			select {
			case commands <- action:
			case <-stop:
			}
		}
		err := cmd.Wait()
		if err != nil {
			logx.Printf("dashboard: %v\n", err)
		}
		done <- err
	}()
	return &Session{snapshots: snapshots, stop: stop, Commands: commands, Done: done}, nil
}

// Smoke exercises the actual Windows form and its first live-data render.
func Smoke() error {
	for _, popup := range []bool{false, true} {
		session, err := Open("ui-check", "fa", popup)
		if err != nil {
			return err
		}
		session.Update(Snapshot{On: true, Adapters: []engine.SlotStats{
			{Name: "Wi-Fi", Present: true, State: "up"},
		}})
		timer := time.NewTimer(30 * time.Second)
		select {
		case command := <-session.Commands:
			if command != "ready" {
				err = fmt.Errorf("unexpected dashboard response: %q", command)
			}
		case err = <-session.Done:
			if err == nil {
				err = fmt.Errorf("dashboard exited before rendering")
			}
		case <-timer.C:
			err = fmt.Errorf("dashboard did not render")
		}
		timer.Stop()
		session.Close()
		if err != nil {
			return fmt.Errorf("popup=%t: %w", popup, err)
		}
	}
	return nil
}
