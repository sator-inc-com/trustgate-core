//go:build !windows

package main

import "os/exec"

// hideCmd is a no-op on non-Windows platforms.
func hideCmd(_ *exec.Cmd) {}

// hideCmdDetached is a no-op on non-Windows platforms.
func hideCmdDetached(_ *exec.Cmd) {}
