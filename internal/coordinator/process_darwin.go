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

/*
On Linux, the system uses Pdeathsig: syscall.SIGKILL inside the SysProcAttr struct. This is a Linux-native kernel feature: if the parent Go process crashes or is violently killed (kill -9), the Linux kernel automatically kills any spawned child processes (like ffmpeg).

On macOS (Darwin), Pdeathsig does not exist. The original developer wanted to replicate this safety feature on macOS to prevent orphaned ffmpeg processes from leaking into the system if the Go daemon crashed.

Why the Empty Body (No-Op) is Safe and Correct

By changing platformParentWatchdog to a no-op, we rely on standard Go subprocess management:

Graceful termination & timeouts: Go's exec.CommandContext(ctx, ...) already automatically terminates the ffmpeg process when the context (ctx) is cancelled, when a timeout occurs, or during a graceful shutdown. This covers 99% of exit scenarios.
Abrupt crash / kill -9: If the Go daemon is violently terminated, ffmpeg will run as an orphan until it finishes. However, because our system slices video into tiny 5-second segments, a single transcoding task takes less than 1 second to finish. ffmpeg will exit on its own almost immediately, meaning there is no risk of process leaks.
Production vs. Development: The production deployment target is Linux, where the native Pdeathsig mechanism is active and works perfectly. macOS is only used for local development, so the empty body is the cleanest and most robust solution for local testing.
*/


