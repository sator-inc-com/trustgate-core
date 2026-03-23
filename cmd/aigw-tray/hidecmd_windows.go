package main

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008
)

// hideCmd sets SysProcAttr so that the command runs without creating a
// visible console window. CREATE_NO_WINDOW prevents the console window
// from appearing when launching console applications (like aigw.exe).
func hideCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// hideCmdDetached is like hideCmd but also detaches the process so it
// survives after the tray app exits. Used for long-running processes.
func hideCmdDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess,
	}
}
