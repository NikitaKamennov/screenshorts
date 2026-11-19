//go:build linux

package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func resources(spec Spec) error {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return fmt.Errorf("Не удалось узнать свободную RAM")
	}
	var available uint64
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			available = value * 1024
		}
	}
	if available < spec.MemoryBytes+1<<30 {
		return fmt.Errorf("Недостаточно свободной RAM для worker и резерва системы")
	}
	var disk syscall.Statfs_t
	if err := syscall.Statfs(spec.Directory, &disk); err != nil {
		return err
	}
	if uint64(disk.Bavail)*uint64(disk.Bsize) < 256<<20 {
		return fmt.Errorf("Для обработки нужно не менее 256 MiB свободного места")
	}
	return nil
}

// hostEnvironment keeps bundled GUI libraries out of systemd's own executables.
func hostEnvironment() []string {
	result := []string{}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "GTK_") ||
			strings.HasPrefix(name, "GIO_") || strings.HasPrefix(name, "GST_") ||
			strings.HasPrefix(name, "WEBKIT_") || strings.HasPrefix(name, "FONTCONFIG_") {
			continue
		}
		result = append(result, value)
	}
	return result
}

func run(ctx context.Context, spec Spec) error {
	systemd, err := exec.LookPath("systemd-run")
	if err != nil {
		return fmt.Errorf("Для защиты worker нужен systemd-run")
	}
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return fmt.Errorf("Для отмены worker нужен systemctl")
	}
	launcher, err := os.Executable()
	if err != nil {
		return fmt.Errorf("Не найден launcher защиты worker: %w", err)
	}
	var bwrap, usr string
	if spec.RuntimeRoot != "" {
		if !filepath.IsAbs(spec.RuntimeRoot) {
			return fmt.Errorf("Нужен абсолютный путь Linux runtime")
		}
		root, err := filepath.EvalSymlinks(spec.RuntimeRoot)
		if err != nil || root == "/" {
			return fmt.Errorf("Некорректный Linux runtime")
		}
		spec.RuntimeRoot = root
		usr = filepath.Join(root, "usr")
		bwrap = filepath.Join(usr, "bin", "bwrap")
		launcher = filepath.Join(root, "bin", "worker-guard")
		for _, path := range []string{bwrap, launcher, filepath.Join(usr, "lib", "x86_64-linux-gnu", "ld-linux-x86-64.so.2")} {
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("Неполный автономный Linux runtime")
			}
		}
	} else {
		bwrap, err = exec.LookPath("bwrap")
		if err != nil {
			return fmt.Errorf("Для изоляции worker нужен bubblewrap")
		}
		bwrap, err = filepath.Abs(bwrap)
		if err != nil {
			return err
		}
		usr = "/usr"
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	unit := "skrinpolka-worker-" + hex.EncodeToString(id[:]) + ".service"
	args := []string{"--user", "--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--expand-environment=no", "--unit=" + unit,
		"--property=CPUQuota=200%", "--property=CPUWeight=10", "--property=MemoryMax=" + strconv.FormatUint(spec.MemoryBytes, 10),
		"--property=MemoryHigh=" + strconv.FormatUint(spec.MemoryBytes*4/5, 10), "--property=MemorySwapMax=0", "--property=TasksMax=32",
		"--property=RuntimeMaxSec=" + strconv.Itoa(spec.TimeoutSeconds), "--property=KillMode=control-group", "--",
		launcher, workerGuardFlag, strconv.FormatUint(spec.MemoryBytes, 10)}
	if spec.RuntimeRoot != "" {
		args = append(args, "--linux-runtime", spec.RuntimeRoot)
	}
	args = append(args, bwrap,
		"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", usr, "/usr", "--dir", "/work", "--bind", spec.Directory, "/work", "--dir", "/runtime",
		"--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/work", "--setenv", "TMPDIR", "/work",
		"--setenv", "OMP_THREAD_LIMIT", strconv.Itoa(spec.Threads), "--setenv", "OMP_NUM_THREADS", strconv.Itoa(spec.Threads),
		"--setenv", "OPENBLAS_NUM_THREADS", strconv.Itoa(spec.Threads), "--setenv", "HF_HUB_OFFLINE", "1", "--setenv", "TRANSFORMERS_OFFLINE", "1")
	if spec.RuntimeRoot != "" {
		args = append(args, "--symlink", "usr/bin", "/bin", "--symlink", "usr/lib", "/lib",
			"--symlink", "usr/lib64", "/lib64", "--setenv", "LD_LIBRARY_PATH", "/usr/lib/x86_64-linux-gnu:/usr/lib")
	} else {
		for _, path := range []string{"/bin", "/lib", "/lib64"} {
			if info, err := os.Lstat(path); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					target, err := os.Readlink(path)
					if err != nil {
						return err
					}
					args = append(args, "--symlink", target, path)
				} else {
					args = append(args, "--ro-bind", path, path)
				}
			}
		}
		if _, err := os.Stat("/etc/ld.so.cache"); err == nil {
			args = append(args, "--dir", "/etc", "--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache")
		}
	}
	mapping := map[string]string{}
	for index, root := range spec.ReadRoots {
		actual, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("Не найден runtime/модель worker: %w", err)
		}
		target := "/runtime/" + strconv.Itoa(index)
		if spec.RuntimeRoot != "" && actual == usr {
			target = "/usr"
		} else {
			args = append(args, "--dir", target, "--ro-bind", actual, target)
		}
		mapping[filepath.Clean(root)] = target
	}
	translate := func(path string) string {
		if relative, err := filepath.Rel(spec.Directory, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Join("/work", relative)
		}
		best, destination := "", ""
		for root, target := range mapping {
			if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, "../") && len(root) > len(best) {
				best, destination = root, filepath.Join(target, relative)
			}
		}
		if destination != "" {
			return destination
		}
		return path
	}
	args = append(args, "--chdir", "/work", "--", translate(spec.Executable))
	for _, arg := range spec.Arguments {
		if filepath.IsAbs(arg) {
			arg = translate(arg)
		}
		args = append(args, arg)
	}
	command := exec.Command(systemd, args...)
	command.Env = hostEnvironment()
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("Не удалось создать защищённый worker: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Защищённый worker завершился с ошибкой; проверь runtime, cgroup и разрешение bubblewrap/user namespaces")
		}
		return nil
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stop := exec.CommandContext(stopCtx, systemctl, "--user", "kill", "--kill-whom=all", "--signal=SIGKILL", unit)
		stop.Env = hostEnvironment()
		stopErr := stop.Run()
		cancel()
		// Retain the shared slot until the process tree has actually exited.
		<-done
		if stopErr != nil {
			return fmt.Errorf("Worker завершён по лимиту; немедленная отмена не подтверждена")
		}
		return ctx.Err()
	}
}
