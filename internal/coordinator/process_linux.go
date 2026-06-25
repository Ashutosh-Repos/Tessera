//go:build linux

package coordinator

import (
	"context"
	"os/exec"
	"syscall"
)

// platformSysProcAttr returns the SysProcAttr for Linux with Pdeathsig to ensure
// child process dies automatically when the parent coordinator dies.
func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

// platformParentWatchdog is a no-op on Linux since the kernel handles it via Pdeathsig.
func platformParentWatchdog(ctx context.Context, cmd *exec.Cmd) {}
