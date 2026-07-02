//go:build darwin

package coordinator

import (
	"context"
	"os/exec"
	"syscall"
)

// platformSysProcAttr returns the SysProcAttr for macOS.
func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// platformParentWatchdog monitors the parent PID on macOS.
// Made a no-op to prevent premature ffmpeg terminations when the daemon itself is run in the background (PPID 1).
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {}

