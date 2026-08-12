//go:build windows

package runtime

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	projectionLockFailImmediately = 0x00000001
	projectionLockExclusive       = 0x00000002
)

var (
	projectionLockFile   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	projectionUnlockFile = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func lockProjectionFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := projectionLockFile.Call(
		file.Fd(), uintptr(projectionLockFailImmediately|projectionLockExclusive), 0,
		1, 0, uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func unlockProjectionFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := projectionUnlockFile.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result == 0 {
		return callErr
	}
	return nil
}
