//go:build !linux

package sandbox

// Windows verifies Job Object limits through its own native adapter.
func WorkerGuard([]string) (bool, int) { return false, 0 }
