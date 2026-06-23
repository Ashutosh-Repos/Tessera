//go:build darwin

package worker

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// platformSysProcAttr returns the SysProcAttr for macOS (no Pdeathsig).
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

// platformLimitProcess lowers CPU priority of the FFmpeg process using renice on macOS.
func platformLimitProcess(pid int) func() {
	cmd := exec.Command("renice", "10", "-p", strconv.Itoa(pid))
	_ = cmd.Run()
	return func() {}
}
