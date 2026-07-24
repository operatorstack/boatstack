//go:build !windows

package boatstack

import "syscall"

// execReplaceProcess replaces the current process image with the target binary.
// On success it never returns; the replacement inherits stdio and environment.
func execReplaceProcess(path string, args []string, env []string) error {
	return syscall.Exec(path, args, env)
}
