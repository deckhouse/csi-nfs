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
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testMountInfo = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
610 22 0:52 / /tmp/root-squash-probe-1 rw,relatime - nfs4 nfs.example.com:/srv/share rw,vers=4.2
611 22 0:53 / /tmp/root\040squash\134probe rw,relatime - nfs nfs.example.com:/srv/other rw,vers=3
`

func TestIsMountPoint(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   bool
	}{
		{"/tmp/root-squash-probe-1", true},
		{"/tmp/root squash\\probe", true},
		{"/tmp/root-squash-probe-2", false},
		{"/tmp", false},
	} {
		got, err := isMountPoint(strings.NewReader(testMountInfo), tc.target)
		if err != nil {
			t.Fatalf("isMountPoint(%q): %v", tc.target, err)
		}
		if got != tc.want {
			t.Errorf("isMountPoint(%q) = %t, want %t", tc.target, got, tc.want)
		}
	}
}

func TestExecMounterIsMountPointReadsTheGivenTable(t *testing.T) {
	table := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(table, []byte(testMountInfo), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ExecMounter{MountInfo: table}.IsMountPoint("/tmp/root-squash-probe-1/")
	if err != nil || !got {
		t.Errorf("IsMountPoint = %t, %v; want true", got, err)
	}
}

func TestUnescapeMountInfo(t *testing.T) {
	for in, want := range map[string]string{
		`/plain`:              "/plain",
		`/a\040b`:             "/a b",
		`/tab\011newline\012`: "/tab\tnewline\n",
		`/back\134slash`:      `/back\slash`,
		`/short\04`:           `/short\04`,
		`/not\999octal`:       `/not\999octal`,
	} {
		if got := unescapeMountInfo(in); got != want {
			t.Errorf("unescapeMountInfo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOSFilesystem(t *testing.T) {
	fsys := OSFilesystem{}
	dir := t.TempDir()

	work, err := fsys.MkdirTemp(dir, DirPrefix+"*")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(work, testFileName)

	report, err := fsys.CreateAndChown(file)
	if err != nil {
		t.Fatalf("CreateAndChown: %v", err)
	}
	if report.UID != uint32(os.Getuid()) {
		t.Errorf("owner uid = %d, want %d", report.UID, os.Getuid())
	}
	// Only root may hand a file to root.
	if os.Getuid() == 0 && report.ChownErr != nil {
		t.Errorf("chown as root: %v", report.ChownErr)
	}
	if os.Getuid() != 0 && !errors.Is(report.ChownErr, fs.ErrPermission) {
		t.Errorf("chown as uid %d = %v, want a permission error", os.Getuid(), report.ChownErr)
	}

	if _, err := fsys.CreateAndChown(file); !errors.Is(err, fs.ErrExist) {
		t.Errorf("a second CreateAndChown = %v, want it to refuse the existing file", err)
	}

	if err := fsys.Rmdir(work); err == nil {
		t.Fatal("Rmdir removed a directory that is not empty")
	}
	if err := fsys.Remove(work); err == nil {
		t.Fatal("Remove removed a directory")
	}

	if err := fsys.Remove(file); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := fsys.Remove(file); err != nil {
		t.Errorf("Remove of a file that is gone = %v, want nil", err)
	}
	if err := fsys.Rmdir(work); err != nil {
		t.Errorf("Rmdir of the empty directory: %v", err)
	}
}

// A helper that outlives the killed command, as mount.nfs outlives mount(8),
// must not keep run waiting on the output pipe.
func TestRunGivesUpOnItsContext(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := run(ctx, "sh", "-c", "sleep 60 & sleep 60")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("run = %v, want the deadline", err)
	}
	// Within WaitDelay: the whole process group was killed, not mount(8) alone.
	if elapsed := time.Since(start); elapsed >= commandWaitDelay {
		t.Errorf("run took %s", elapsed)
	}
}

func TestRunReportsTheOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run")
	}
	err := run(context.Background(), "sh", "-c", "echo 'mount.nfs: access denied by server' >&2; exit 32")
	if err == nil || !strings.Contains(err.Error(), "access denied by server") {
		t.Errorf("run = %v, want the output of the command in the error", err)
	}
}
