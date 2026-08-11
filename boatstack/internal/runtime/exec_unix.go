//go:build !windows

package runtime

import "syscall"

func execute(path string, arguments, environment []string) (int, error) {
	if err := syscall.Exec(path, arguments, environment); err != nil {
		return 1, err
	}
	return 0, nil
}
