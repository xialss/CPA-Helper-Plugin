//go:build windows

package snapshot

// Windows does not expose a portable parent-directory fsync through os.File.
// atomicWrite still flushes file contents before the same-volume replacement.
func syncDirectory(string) error { return nil }
