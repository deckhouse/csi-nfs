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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	testWorkDir  = "/work"
	testFileName = ".probe-file"
)

var testTarget = Target{
	Host:       "nfs.example.com",
	Share:      "/srv/share",
	NFSVersion: "4.2",
	FileName:   testFileName,
}

// fakeMounter keeps the mount table in memory.
type fakeMounter struct {
	mu sync.Mutex

	// mountErr is what Mount returns; mountAnyway mounts the target even then,
	// like a mount(8) killed after the kernel had completed the mount.
	mountErr    error
	mountAnyway bool
	// mountHangs makes Mount wait for its context.
	mountHangs bool
	unmountErr error
	isMountErr error

	mounted  map[string]bool
	mounts   []string
	unmounts []string
	options  []string
}

func (m *fakeMounter) Mount(ctx context.Context, source, target string, options []string) error {
	m.mu.Lock()
	m.mounts = append(m.mounts, source)
	m.options = options
	hangs := m.mountHangs
	m.mu.Unlock()

	if hangs {
		<-ctx.Done()
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mountErr == nil || m.mountAnyway {
		m.setMounted(target, true)
	}
	return m.mountErr
}

func (m *fakeMounter) Unmount(_ context.Context, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unmounts = append(m.unmounts, target)
	if m.unmountErr != nil {
		return m.unmountErr
	}
	m.setMounted(target, false)
	return nil
}

func (m *fakeMounter) IsMountPoint(target string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mounted[target], m.isMountErr
}

func (m *fakeMounter) setMounted(target string, mounted bool) {
	if m.mounted == nil {
		m.mounted = map[string]bool{}
	}
	m.mounted[target] = mounted
}

// fakeFS keeps the directories of the work dir and the files of the export in
// memory.
type fakeFS struct {
	mu sync.Mutex

	dirs  map[string]bool
	files map[string]bool
	next  int

	report    FileReport
	createErr error
	// createHangs blocks CreateAndChown until release is closed.
	createHangs bool
	release     chan struct{}
	// removeErrs is returned by the successive Remove calls.
	removeErrs []error

	removes []string
	rmdirs  []string
}

func newFakeFS() *fakeFS {
	return &fakeFS{dirs: map[string]bool{}, files: map[string]bool{}, release: make(chan struct{})}
}

func (f *fakeFS) MkdirTemp(dir, pattern string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	name := filepath.Join(dir, strings.Replace(pattern, "*", string(rune('a'+f.next-1)), 1))
	f.dirs[name] = true
	return name, nil
}

func (f *fakeFS) ReadDir(dir string) ([]os.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []os.DirEntry
	for d := range f.dirs {
		if filepath.Dir(d) == dir {
			out = append(out, fakeEntry{name: filepath.Base(d), dir: true})
		}
	}
	for p := range f.files {
		if filepath.Dir(p) == dir {
			out = append(out, fakeEntry{name: filepath.Base(p)})
		}
	}
	return out, nil
}

func (f *fakeFS) Rmdir(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rmdirs = append(f.rmdirs, path)
	delete(f.dirs, path)
	return nil
}

func (f *fakeFS) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, path)
	if len(f.removeErrs) > 0 {
		err := f.removeErrs[0]
		f.removeErrs = f.removeErrs[1:]
		if err != nil {
			return err
		}
	}
	delete(f.files, path)
	return nil
}

func (f *fakeFS) CreateAndChown(path string) (FileReport, error) {
	if f.createHangs {
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return FileReport{}, f.createErr
	}
	f.files[path] = true
	return f.report, nil
}

type fakeEntry struct {
	name string
	dir  bool
}

func (e fakeEntry) Name() string { return e.name }
func (e fakeEntry) IsDir() bool  { return e.dir }
func (e fakeEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}
func (e fakeEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not implemented") }

func newProber(m *fakeMounter, f *fakeFS) *Prober {
	return &Prober{
		Mounter:        m,
		FS:             f,
		WorkDir:        testWorkDir,
		MountTimeout:   time.Second,
		IOTimeout:      time.Second,
		UnmountTimeout: time.Second,
		Log:            logger.NewNop(),
	}
}

func permissionErr(op string) error {
	return &fs.PathError{Op: op, Path: testFileName, Err: syscall.EPERM}
}

// assertReleased checks the probe left nothing behind: the mount point is
// unmounted and removed, and no probe file stays on the export.
func assertReleased(t *testing.T, m *fakeMounter, f *fakeFS) {
	t.Helper()
	for target, mounted := range m.mounted {
		if mounted {
			t.Errorf("%s is still mounted", target)
		}
	}
	if len(f.dirs) != 0 {
		t.Errorf("mount points left in the work dir: %v", f.dirs)
	}
	if len(f.files) != 0 {
		t.Errorf("probe files left on the export: %v", f.files)
	}
}

func assertResult(t *testing.T, got Result, status metav1.ConditionStatus, reason string) {
	t.Helper()
	if got.Status != status || got.Reason != reason {
		t.Fatalf("result = %s/%s (%s), want %s/%s", got.Status, got.Reason, got.Message, status, reason)
	}
	if got.Message == "" {
		t.Error("the result needs a message")
	}
}

func TestProbeRootNotSquashed(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	f.report = FileReport{UID: 0, GID: 0}

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionFalse, v1alpha1.ReasonRootNotSquashed)
	assertReleased(t, m, f)
	if want := []string{"nfs.example.com:/srv/share"}; !slices.Equal(m.mounts, want) {
		t.Errorf("mounts = %v, want %v", m.mounts, want)
	}
}

// The verdict follows the chown, not the owner the client shows: with NFSv4
// a client in another ID-mapping domain shows root's own file as nobody.
func TestProbeRootNotSquashedDespiteMappedOwner(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	f.report = FileReport{UID: 65534, GID: 65534}

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionFalse, v1alpha1.ReasonRootNotSquashed)
	if !strings.Contains(got.Message, "65534:65534") {
		t.Errorf("message %q does not name the owner the client sees", got.Message)
	}
}

func TestProbeRootSquashed(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	f.report = FileReport{UID: 65534, GID: 65534, ChownErr: permissionErr("chown")}

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionTrue, v1alpha1.ReasonRootSquashed)
	if !strings.Contains(got.Message, "65534:65534") || !strings.Contains(got.Message, "no_root_squash") {
		t.Errorf("message %q must name the owner and the fix", got.Message)
	}
	assertReleased(t, m, f)
}

func TestProbeRootWriteDenied(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		t.Run(errno.Error(), func(t *testing.T) {
			m, f := &fakeMounter{}, newFakeFS()
			f.createErr = &fs.PathError{Op: "open", Path: testFileName, Err: errno}

			got := newProber(m, f).Probe(context.Background(), testTarget)

			assertResult(t, got, metav1.ConditionTrue, v1alpha1.ReasonRootWriteDenied)
			assertReleased(t, m, f)
		})
	}
}

func TestProbeUnknown(t *testing.T) {
	for _, tc := range []struct {
		name      string
		createErr error
		chownErr  error
		reason    string
	}{
		{"read-only export", &fs.PathError{Op: "open", Path: testFileName, Err: syscall.EROFS}, nil, v1alpha1.ReasonExportReadOnly},
		{"create fails", &fs.PathError{Op: "open", Path: testFileName, Err: syscall.EIO}, nil, v1alpha1.ReasonProbeFailed},
		{"chown fails", nil, &fs.PathError{Op: "chown", Path: testFileName, Err: syscall.EIO}, v1alpha1.ReasonProbeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := &fakeMounter{}, newFakeFS()
			f.createErr = tc.createErr
			f.report = FileReport{ChownErr: tc.chownErr}

			got := newProber(m, f).Probe(context.Background(), testTarget)

			assertResult(t, got, metav1.ConditionUnknown, tc.reason)
			assertReleased(t, m, f)
		})
	}
}

func TestProbeMountFailed(t *testing.T) {
	m, f := &fakeMounter{mountErr: errors.New("mount.nfs: access denied by server")}, newFakeFS()

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionUnknown, v1alpha1.ReasonMountFailed)
	for _, want := range []string{"nfs.example.com:/srv/share", "nfsvers=4.2", "access denied by server"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message %q does not contain %q", got.Message, want)
		}
	}
	if len(m.unmounts) != 0 {
		t.Errorf("unmounted %v, though nothing was mounted", m.unmounts)
	}
	assertReleased(t, m, f)
}

func TestProbeMountTimesOut(t *testing.T) {
	m, f := &fakeMounter{mountHangs: true}, newFakeFS()
	p := newProber(m, f)
	p.MountTimeout = 50 * time.Millisecond

	start := time.Now()
	got := p.Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionUnknown, v1alpha1.ReasonMountFailed)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the probe took %s, the mount timeout is %s", elapsed, p.MountTimeout)
	}
	assertReleased(t, m, f)
}

// mount(8) killed on its timeout after the kernel completed the mount: the
// result of Mount is not trusted, the mount table is.
func TestProbeUnmountsAMountThatReportedFailure(t *testing.T) {
	m, f := &fakeMounter{mountErr: context.DeadlineExceeded, mountAnyway: true}, newFakeFS()

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionUnknown, v1alpha1.ReasonMountFailed)
	if len(m.unmounts) != 1 {
		t.Errorf("unmounts = %v, want one", m.unmounts)
	}
	assertReleased(t, m, f)
}

func TestProbeFileOperationsTimeOut(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	f.createHangs = true
	defer close(f.release)
	p := newProber(m, f)
	p.IOTimeout = 50 * time.Millisecond

	got := p.Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionUnknown, v1alpha1.ReasonProbeFailed)
	if len(m.unmounts) != 1 {
		t.Errorf("unmounts = %v, want one", m.unmounts)
	}
	if len(f.dirs) != 0 {
		t.Errorf("mount points left in the work dir: %v", f.dirs)
	}
}

// The directory of a mount that could not be released is the export itself:
// it must stay, not be removed.
func TestProbeKeepsADirectoryThatIsStillMounted(t *testing.T) {
	m, f := &fakeMounter{unmountErr: errors.New("device is busy")}, newFakeFS()

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionFalse, v1alpha1.ReasonRootNotSquashed)
	if len(f.rmdirs) != 0 {
		t.Errorf("removed %v while it was still mounted", f.rmdirs)
	}
}

func TestProbeReportsAFileItCouldNotRemove(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	f.report = FileReport{UID: 65534, GID: 65534, ChownErr: permissionErr("chown")}
	f.removeErrs = []error{nil, &fs.PathError{Op: "unlink", Path: testFileName, Err: syscall.EIO}}

	got := newProber(m, f).Probe(context.Background(), testTarget)

	assertResult(t, got, metav1.ConditionTrue, v1alpha1.ReasonRootSquashed)
	if !strings.Contains(got.Message, testFileName) || !strings.Contains(got.Message, "could not be removed") {
		t.Errorf("message %q does not report the file left on the export", got.Message)
	}
}

// The probe file has a fixed name, so the next probe removes what an earlier
// one left behind before creating it again.
func TestProbeRemovesTheFileOfAnEarlierProbeFirst(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	file := filepath.Join(testWorkDir, DirPrefix+"a", testFileName)
	f.files[file] = true

	newProber(m, f).Probe(context.Background(), testTarget)

	if want := []string{file, file}; !slices.Equal(f.removes, want) {
		t.Errorf("removes = %v, want %v (before and after creating)", f.removes, want)
	}
	assertReleased(t, m, f)
}

func TestSweep(t *testing.T) {
	m, f := &fakeMounter{}, newFakeFS()
	mounted := filepath.Join(testWorkDir, DirPrefix+"old1")
	plain := filepath.Join(testWorkDir, DirPrefix+"old2")
	foreign := filepath.Join(testWorkDir, "something-else")
	f.dirs[mounted], f.dirs[plain], f.dirs[foreign] = true, true, true
	m.setMounted(mounted, true)

	newProber(m, f).Sweep()

	if want := []string{mounted}; !slices.Equal(m.unmounts, want) {
		t.Errorf("unmounts = %v, want %v", m.unmounts, want)
	}
	if !f.dirs[foreign] {
		t.Error("a directory the probe does not own was removed")
	}
	if f.dirs[mounted] || f.dirs[plain] {
		t.Errorf("mount points of an earlier run were not removed: %v", f.dirs)
	}
}

func TestMountOptions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		target     Target
		tlsEnabled bool
		want       string
	}{
		{"NFSv3", Target{NFSVersion: "3"}, true, "nfsvers=3,soft,timeo=50,retrans=2,retry=0,nolock"},
		{"NFSv4.1", Target{NFSVersion: "4.1"}, true, "nfsvers=4.1,soft,timeo=50,retrans=2,retry=0"},
		{"TLS", Target{NFSVersion: "4.2", TLS: true}, true, "nfsvers=4.2,xprtsec=tls,soft,timeo=50,retrans=2,retry=0"},
		{"mTLS", Target{NFSVersion: "4.2", TLS: true, MTLS: true}, true, "nfsvers=4.2,xprtsec=mtls,soft,timeo=50,retrans=2,retry=0"},
		{"TLS outside the edition", Target{NFSVersion: "4.2", TLS: true, MTLS: true}, false, "nfsvers=4.2,soft,timeo=50,retrans=2,retry=0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(MountOptions(tc.target, tc.tlsEnabled), ","); got != tc.want {
				t.Errorf("MountOptions = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSource(t *testing.T) {
	for _, tc := range []struct{ host, share, want string }{
		{"nfs.example.com", "/srv/share", "nfs.example.com:/srv/share"},
		{"192.0.2.10", "srv/share/", "192.0.2.10:/srv/share"},
		{"2001:db8::10", "/srv/share", "[2001:db8::10]:/srv/share"},
	} {
		if got := Source(tc.host, tc.share); got != tc.want {
			t.Errorf("Source(%q, %q) = %q, want %q", tc.host, tc.share, got, tc.want)
		}
	}
}
