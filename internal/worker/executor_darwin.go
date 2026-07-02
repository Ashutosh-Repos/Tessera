//go:build darwin

package worker

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

// platformSysProcAttr returns the SysProcAttr for macOS (no Pdeathsig).
func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// platformParentWatchdog monitors the parent PID on macOS.
// Made a no-op to prevent premature ffmpeg terminations when the daemon itself is run in the background (PPID 1).
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {}


// platformLimitProcess lowers CPU priority of the FFmpeg process using renice on macOS.
func platformLimitProcess(pid int) func() {
	cmd := exec.Command("renice", "10", "-p", strconv.Itoa(pid))
	_ = cmd.Run()
	return func() {}
}
