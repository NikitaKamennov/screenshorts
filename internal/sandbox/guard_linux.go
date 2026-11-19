//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const workerGuardFlag = "--skrinpolka-internal-worker-guard"

// WorkerGuard runs before desktop startup in a transient systemd service. The
// engine is not executed unless the kernel exposes finite limits for this very
// process. A successful systemd-run invocation alone is not proof of protection.
func WorkerGuard(arguments []string) (bool, int) {
	if len(arguments) == 0 || arguments[0] != workerGuardFlag {
		return false, 0
	}
	if len(arguments) < 3 {
		return true, 125
	}
	memory, err := strconv.ParseUint(arguments[1], 10, 64)
	if err != nil || memory < 128<<20 || memory > 4<<30 {
		return true, 125
	}
	if err := verifyCurrentCgroup(memory); err != nil {
		fmt.Fprintln(os.Stderr, "Не подтверждены ограничения cgroup v2; worker не запущен")
		return true, 125
	}
	executable, args := arguments[2], arguments[2:]
	if executable == "--linux-runtime" {
		if len(arguments) < 5 || !filepath.IsAbs(arguments[3]) {
			return true, 125
		}
		root, err := filepath.EvalSymlinks(arguments[3])
		if err != nil || root == "/" {
			return true, 125
		}
		bwrap := filepath.Join(root, "usr", "bin", "bwrap")
		if arguments[4] != bwrap {
			return true, 125
		}
		executable = filepath.Join(root, "usr", "lib", "x86_64-linux-gnu", "ld-linux-x86-64.so.2")
		args = append([]string{executable, "--library-path", filepath.Join(root, "usr", "lib", "x86_64-linux-gnu") + ":" + filepath.Join(root, "usr", "lib")}, arguments[4:]...)
		// A distribution's bwrap may carry an AppArmor execution profile.
		// Prefer that entry point when present; the bundled copy is still the
		// fallback on systems which permit unprivileged namespaces directly.
		if info, err := os.Stat("/usr/bin/bwrap"); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			executable = "/usr/bin/bwrap"
			args = append([]string{executable}, arguments[5:]...)
		}
	} else if !filepath.IsAbs(executable) {
		return true, 125
	}
	if err := syscall.Exec(executable, args, hostEnvironment()); err != nil {
		return true, 126
	}
	return true, 0
}

func verifyCurrentCgroup(memory uint64) error {
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return err
	}
	group := ""
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "0::/") {
			path := strings.TrimPrefix(line, "0::")
			if filepath.Clean(path) != path || path == "/" {
				return fmt.Errorf("некорректная группа worker")
			}
			group = strings.TrimPrefix(path, "/")
			break
		}
	}
	if group == "" {
		return fmt.Errorf("worker требует cgroup v2")
	}
	root, err := os.OpenRoot("/sys/fs/cgroup")
	if err != nil {
		return err
	}
	defer root.Close()
	read := func(name string) (string, error) {
		data, err := root.ReadFile(filepath.Join(group, name))
		return strings.TrimSpace(string(data)), err
	}
	limit := func(name string, maximum uint64, allowZero bool) error {
		value, err := read(name)
		if err != nil {
			return err
		}
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil || number > maximum || (!allowZero && number == 0) {
			return fmt.Errorf("лимит %s не подтверждён", name)
		}
		return nil
	}
	for _, item := range []struct {
		name      string
		maximum   uint64
		allowZero bool
	}{
		{"memory.max", memory, false},
		{"memory.high", memory, false},
		{"memory.swap.max", 0, true},
		{"pids.max", 32, false},
	} {
		if err := limit(item.name, item.maximum, item.allowZero); err != nil {
			return err
		}
	}
	cpu, err := read("cpu.max")
	if err != nil {
		return err
	}
	parts := strings.Fields(cpu)
	if len(parts) != 2 {
		return fmt.Errorf("нет ограничения CPU")
	}
	quota, quotaErr := strconv.ParseUint(parts[0], 10, 64)
	period, periodErr := strconv.ParseUint(parts[1], 10, 64)
	if quotaErr != nil || periodErr != nil || period < 1000 || period > 1000000 || quota == 0 || quota > 2*period {
		return fmt.Errorf("нет ограничения CPU до 200%%")
	}
	return nil
}
