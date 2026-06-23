//go:build linux

package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// platformSysProcAttr returns the SysProcAttr for Linux with Pdeathsig.
func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL, // Kill FFmpeg if parent worker dies
	}
}

// platformParentWatchdog is a no-op on Linux (Pdeathsig handles it).
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {}

// platformLimitProcess limits the resource usage of the FFmpeg process using cgroups v2.
func platformLimitProcess(pid int) func() {
	cgroupPath := fmt.Sprintf("/sys/fs/cgroup/transcoder/task-%d", pid)
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return func() {} // Fallback if no permissions to write to /sys/fs/cgroup
	}

	// Limit memory to 1.5GB
	_ = os.WriteFile(filepath.Join(cgroupPath, "memory.max"), []byte("1500M"), 0644)
	
	// Lower CPU priority
	_ = os.WriteFile(filepath.Join(cgroupPath, "cpu.weight"), []byte("50"), 0644)

	// Add PID to the cgroup
	_ = os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644)

	return func() {
		// Clean up the task cgroup directory
		_ = os.Remove(cgroupPath)
	}
}
