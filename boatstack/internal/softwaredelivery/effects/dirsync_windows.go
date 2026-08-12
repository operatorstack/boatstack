//go:build windows

package effects

// replaceFile uses MoveFileExW with MOVEFILE_WRITE_THROUGH on Windows. Go's
// ordinary directory handle cannot be flushed portably, so no weaker second
// flush is attempted here.
func syncDirectory(string) error { return nil }
