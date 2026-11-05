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

// Package probe tells whether an NFS server squashes root on an export.
//
// The export options are not visible to a client, so the only way to learn
// them is to act as root and see what the server does: the probe mounts the
// export, creates a file in its root and asks the server to hand that file to
// root:root. A server that keeps root as root lets it; a server with
// root_squash or all_squash has already created the file as another user and
// refuses the chown, or refuses root the file in the first place.
//
// The verdict rests on the chown rather than on the owner the client sees:
// with NFSv4 the owner arrives as a name, and a client whose ID-mapping domain
// differs from the server's shows root's own file as nobody. The chown is also
// what the kubelet does for fsGroup, one of the operations root_squash breaks.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/logger"
)

// DirPrefix starts the name of every local mount point of the probe, so that
// the ones a killed container left behind can be told apart in the work
// directory.
const DirPrefix = "root-squash-probe-"

// Target is the export of one NFSStorageClass.
type Target struct {
	Host       string
	Share      string
	NFSVersion string
	TLS        bool
	MTLS       bool

	// FileName is the name of the probe file in the export root. It is fixed
	// per NFSStorageClass, so a file a failed probe left behind is removed by
	// the next probe instead of piling up next to the volumes.
	FileName string
}

// Result is the verdict of one probe, in the terms of the RootSquash condition.
type Result struct {
	Status  metav1.ConditionStatus
	Reason  string
	Message string
}

// Mounter mounts and unmounts the export. Mount and Unmount give up when ctx
// is done.
type Mounter interface {
	Mount(ctx context.Context, source, target string, options []string) error
	// Unmount returns nil only once target is no longer a mount point.
	Unmount(ctx context.Context, target string) error
	IsMountPoint(target string) (bool, error)
}

// FileReport is what the probe learnt from the file it created.
type FileReport struct {
	// UID and GID are the owner of the file as the NFS client sees it.
	UID, GID uint32
	// ChownErr is the answer of the server to handing the file to root:root.
	ChownErr error
}

// Filesystem is the part of the local filesystem the probe touches.
//
// It deliberately has no recursive removal: the probe directory is the mount
// point of the export, and a recursive removal while the export is still
// mounted would delete the data on the export.
type Filesystem interface {
	MkdirTemp(dir, pattern string) (string, error)
	ReadDir(dir string) ([]os.DirEntry, error)
	// Rmdir removes an empty directory.
	Rmdir(path string) error
	// Remove removes a file; a file that is not there is not an error.
	Remove(path string) error
	// CreateAndChown creates path exclusively, reads its owner, asks for it
	// to be handed to root:root and closes it.
	CreateAndChown(path string) (FileReport, error)
}

// Prober runs probes. It holds no state between them.
type Prober struct {
	Mounter Mounter
	FS      Filesystem
	// WorkDir holds the local mount points.
	WorkDir string
	// TLSEnabled is whether the edition supports RPC-with-TLS. The module
	// controller puts xprtsec into the mount options of the class only then,
	// and the probe has to mount the export the way the volumes are mounted.
	TLSEnabled bool

	MountTimeout   time.Duration
	IOTimeout      time.Duration
	UnmountTimeout time.Duration

	Log logger.Logger
}

// Probe checks one export. It never fails: anything that keeps it from a
// verdict is an Unknown result with a reason.
func (p *Prober) Probe(ctx context.Context, t Target) Result {
	dir, err := p.FS.MkdirTemp(p.WorkDir, DirPrefix+"*")
	if err != nil {
		return unknown(v1alpha1.ReasonProbeFailed, fmt.Sprintf("Unable to create a local mount point: %v.", err))
	}
	defer p.release(dir)

	source := Source(t.Host, t.Share)
	options := MountOptions(t, p.TLSEnabled)

	mountCtx, cancel := context.WithTimeout(ctx, p.MountTimeout)
	err = p.Mounter.Mount(mountCtx, source, dir, options)
	cancel()
	if err != nil {
		return unknown(v1alpha1.ReasonMountFailed,
			fmt.Sprintf("Unable to mount %s with options %s: %v.", source, strings.Join(options, ","), err))
	}

	return p.inspect(ctx, dir, t.FileName)
}

// Sweep releases the mount points a previous run of the container left in the
// work directory. The mounts themselves die with the mount namespace of the
// container, but the directories live in an emptyDir and outlast it.
func (p *Prober) Sweep() {
	entries, err := p.FS.ReadDir(p.WorkDir)
	if err != nil {
		p.Log.Warn("unable to list the work directory", "dir", p.WorkDir, "err", err.Error())
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), DirPrefix) {
			p.release(filepath.Join(p.WorkDir, e.Name()))
		}
	}
}

// inspect does the file operations on the mounted export.
//
// The mount is soft with short timeouts, so a server that stops answering
// fails them, but only after timeo*retrans per call; IOTimeout bounds the
// whole sequence regardless. On a timeout the goroutine is left to finish on
// its own: a system call cannot be interrupted, and the forced lazy unmount
// in release detaches the mount underneath it.
func (p *Prober) inspect(ctx context.Context, dir, name string) Result {
	ioCtx, cancel := context.WithTimeout(ctx, p.IOTimeout)
	defer cancel()

	done := make(chan Result, 1)
	go func() { done <- p.check(filepath.Join(dir, name), name) }()

	select {
	case res := <-done:
		return res
	case <-ioCtx.Done():
		return unknown(v1alpha1.ReasonProbeFailed,
			fmt.Sprintf("The export was mounted, but did not complete the file operations within %s.", p.IOTimeout))
	}
}

func (p *Prober) check(file, name string) Result {
	// A file of an earlier probe that could not be removed then. Exclusive
	// creation below reports it if this removal fails too.
	if err := p.FS.Remove(file); err != nil {
		p.Log.Warn("unable to remove the probe file of an earlier probe", "file", name, "err", err.Error())
	}

	report, err := p.FS.CreateAndChown(file)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return Result{
			Status: metav1.ConditionTrue,
			Reason: v1alpha1.ReasonRootWriteDenied,
			Message: fmt.Sprintf("The NFS server refused root to create a file in the export root (%v): "+
				"the export maps root to another user (root_squash or all_squash). "+
				"Provisioning, deletion and cleanup of volumes of this class fail; export the share with no_root_squash.", err),
		}
	case errors.Is(err, syscall.EROFS):
		return unknown(v1alpha1.ReasonExportReadOnly,
			"The export is read-only, so it cannot be checked for root squashing, and volumes of this class cannot be provisioned on it.")
	case err != nil:
		return unknown(v1alpha1.ReasonProbeFailed, fmt.Sprintf("Unable to create the probe file %s in the export root: %v.", name, err))
	}

	res := verdict(report)
	if err := p.FS.Remove(file); err != nil {
		res.Message += fmt.Sprintf(" The probe file %s could not be removed from the export root (%v); the next check removes it, or delete it by hand.", name, err)
	}
	return res
}

func verdict(r FileReport) Result {
	switch {
	case r.ChownErr == nil:
		return Result{
			Status: metav1.ConditionFalse,
			Reason: v1alpha1.ReasonRootNotSquashed,
			Message: fmt.Sprintf("The NFS server keeps root as root on the export: it let root hand the file root created to root "+
				"(the client sees the file owned by %d:%d).", r.UID, r.GID),
		}
	case errors.Is(r.ChownErr, fs.ErrPermission):
		return Result{
			Status: metav1.ConditionTrue,
			Reason: v1alpha1.ReasonRootSquashed,
			Message: fmt.Sprintf("The NFS server maps root to another user on the export (root_squash or all_squash): "+
				"a file root created is owned by %d:%d, and the server refused to hand it to root (%v). "+
				"Deletion and cleanup of volumes of this class and fsGroup ownership changes fail; export the share with no_root_squash.",
				r.UID, r.GID, r.ChownErr),
		}
	default:
		return unknown(v1alpha1.ReasonProbeFailed, fmt.Sprintf("Unable to hand the probe file to root: %v.", r.ChownErr))
	}
}

// release unmounts dir and removes it. The directory is removed only once it
// is no longer a mount point: until then it is the export itself.
func (p *Prober) release(dir string) {
	mounted, err := p.Mounter.IsMountPoint(dir)
	if err != nil {
		p.Log.Warn("unable to tell whether the probe directory is mounted, unmounting it anyway", "dir", dir, "err", err.Error())
	}
	// A mount that timed out may still have completed after mount(8) was
	// killed, so this does not trust the result of Mount.
	if mounted || err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.UnmountTimeout)
		err := p.Mounter.Unmount(ctx, dir)
		cancel()
		if err != nil {
			p.Log.Error(fmt.Sprintf("unable to unmount the export from %s, leaving the probe directory in place", dir), logger.Err(err))
			return
		}
	}
	if err := p.FS.Rmdir(dir); err != nil {
		p.Log.Warn("unable to remove the probe directory", "dir", dir, "err", err.Error())
	}
}

// Source is the export the way mount.nfs takes it. Like the CSI driver, it
// mounts the share from its root and brackets an IPv6 address.
func Source(host, share string) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		host = "[" + host + "]"
	}
	return host + ":" + path.Join("/", share)
}

// MountOptions are the options the class mounts its volumes with as far as
// they decide how the server sees the client -- the version and RPC-with-TLS,
// gated on the edition like GetSCMountOptions of the module controller --
// plus the probe's own. The mount mode, timeouts and read-only flag of the
// class are left out: they change how the client waits, not what the server
// allows root.
func MountOptions(t Target, tlsEnabled bool) []string {
	opts := []string{"nfsvers=" + t.NFSVersion}
	if tlsEnabled {
		switch {
		case t.MTLS:
			opts = append(opts, "xprtsec=mtls")
		case t.TLS:
			opts = append(opts, "xprtsec=tls")
		}
	}
	// soft with a short timeo makes a server that stops answering fail the
	// call rather than hang it for as long as it is away, which is what a hard
	// mount does. retry=0 makes mount.nfs give up after the first failure
	// instead of retrying in the foreground for two minutes.
	opts = append(opts, "soft", "timeo=50", "retrans=2", "retry=0")
	if t.NFSVersion == "3" {
		// The probe takes no locks, so it need not depend on rpc.statd of the node.
		opts = append(opts, "nolock")
	}
	return opts
}

func unknown(reason, message string) Result {
	return Result{Status: metav1.ConditionUnknown, Reason: reason, Message: message}
}
