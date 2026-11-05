/*
Copyright 2026 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package probe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	procMountInfo = "/proc/self/mountinfo"

	// How long Wait may still block on the output pipes of a command killed on
	// its context. mount(8) runs mount.nfs as a child, and a child that
	// survives the kill holds the pipes open.
	commandWaitDelay = 5 * time.Second
)

// ExecMounter mounts with mount(8), so that mount.nfs resolves the server,
// negotiates the version and, for NFSv3, talks to the MOUNT service the same
// way it does for the volumes of the CSI driver.
type ExecMounter struct {
	// MountInfo is the mount table to read; /proc/self/mountinfo when empty.
	MountInfo string
}

func (m ExecMounter) Mount(ctx context.Context, source, target string, options []string) error {
	return run(ctx, "mount", "-t", "nfs", "-o", strings.Join(options, ","), source, target)
}

// Unmount tries a regular unmount first -- for NFSv3 it also tells the MOUNT
// service the client is gone -- and falls back to a forced lazy unmount, which
// aborts the calls in flight and detaches the mount at once, whether the
// server answers or not.
func (m ExecMounter) Unmount(ctx context.Context, target string) error {
	umountErr := run(ctx, "umount", target)
	if umountErr == nil {
		return nil
	}
	// EINVAL: target is not a mount point (any more).
	if err := unix.Unmount(target, unix.MNT_FORCE|unix.MNT_DETACH); err != nil && !errors.Is(err, unix.EINVAL) {
		return errors.Join(umountErr, fmt.Errorf("forced lazy unmount: %w", err))
	}
	mounted, err := m.IsMountPoint(target)
	if err != nil {
		return errors.Join(umountErr, err)
	}
	if mounted {
		return fmt.Errorf("%s is still mounted: %w", target, umountErr)
	}
	return nil
}

// IsMountPoint reads the mount table instead of calling stat on target, which
// would hang on an export whose server is gone.
func (m ExecMounter) IsMountPoint(target string) (bool, error) {
	name := m.MountInfo
	if name == "" {
		name = procMountInfo
	}
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	return isMountPoint(f, filepath.Clean(target))
}

// isMountPoint reports whether target is the mount point (the fifth field) of
// a line of a mountinfo table.
func isMountPoint(r io.Reader, target string) (bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 {
			continue
		}
		if unescapeMountInfo(fields[4]) == target {
			return true, nil
		}
	}
	return false, sc.Err()
}

// unescapeMountInfo undoes the octal escapes the kernel writes into mountinfo
// for a space, a tab, a newline and a backslash.
func unescapeMountInfo(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Kill the whole process group on the context, not mount(8) alone.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = commandWaitDelay

	out, err := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s did not finish in time: %w", name, ctxErr)
	}
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// OSFilesystem is the Filesystem of the container.
type OSFilesystem struct{}

func (OSFilesystem) MkdirTemp(dir, pattern string) (string, error) {
	return os.MkdirTemp(dir, pattern)
}

func (OSFilesystem) ReadDir(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}

// Rmdir is rmdir(2) rather than os.Remove, which would unlink a file too.
func (OSFilesystem) Rmdir(path string) error {
	if err := unix.Rmdir(path); err != nil {
		return &os.PathError{Op: "rmdir", Path: path, Err: err}
	}
	return nil
}

// Remove is unlink(2): it never removes a directory.
func (OSFilesystem) Remove(path string) error {
	if err := unix.Unlink(path); err != nil && !errors.Is(err, unix.ENOENT) {
		return &os.PathError{Op: "unlink", Path: path, Err: err}
	}
	return nil
}

func (OSFilesystem) CreateAndChown(path string) (report FileReport, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return FileReport{}, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return FileReport{}, &os.PathError{Op: "fstat", Path: path, Err: err}
	}
	return FileReport{UID: st.Uid, GID: st.Gid, ChownErr: f.Chown(0, 0)}, nil
}
