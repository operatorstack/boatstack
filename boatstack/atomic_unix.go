//go:build !windows

package boatstack

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
