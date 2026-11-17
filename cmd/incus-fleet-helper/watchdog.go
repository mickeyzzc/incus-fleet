package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// startWatchdog launches a detached rollback timer. Confirmation removes the
// state file, so the timer becomes a no-op after a successful confirm.
func startWatchdog(taskID string, seconds int) {
	if taskID == "" || seconds <= 0 {
		return
	}
	delay := watchdogDelay(seconds)
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer func() { _ = devnull.Close() }()
	cmd := exec.Command(os.Args[0], "net-watchdog", "--task", taskID, "--delay", strconv.Itoa(delay)) //nolint:gosec // re-exec of the installed helper
	cmd.Env = append(os.Environ(), "INCUS_FLEET_HELPER_STATE_DIR="+stateDir())
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to start rollback watchdog:", err)
		return
	}
	_ = cmd.Process.Release()
}

func watchdogDelay(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return seconds
}

func runWatchdog(args []string) {
	taskID := ""
	delay := 120
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task":
			if i+1 < len(args) {
				taskID = args[i+1]
				i++
			}
		case "--delay":
			if i+1 < len(args) {
				if parsed, err := strconv.Atoi(args[i+1]); err == nil {
					delay = parsed
				}
				i++
			}
		}
	}
	if taskID == "" {
		return
	}
	time.Sleep(time.Duration(delay) * time.Second)
	if _, err := os.Stat(statePath(taskID)); err != nil {
		return
	}
	if err := rollbackState(taskID); err != nil {
		fmt.Fprintln(os.Stderr, "automatic rollback failed:", err)
	}
}
