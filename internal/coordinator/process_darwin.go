//go:build darwin

package coordinator

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// platformSysProcAttr returns the SysProcAttr for macOS.
func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// platformParentWatchdog monitors the parent PID on macOS.
// If the parent changes to PID 1 (launchd), FFmpeg is orphaned and must be killed.
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if os.Getppid() == 1 {
				// Parent died and process became orphaned (parent PID 1) — kill FFmpeg process group
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return
			}
		}
	}
}
