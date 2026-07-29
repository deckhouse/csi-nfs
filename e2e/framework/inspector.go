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

package framework

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	storagekube "github.com/deckhouse/storage-e2e/pkg/kubernetes"
)

// An Inspector is a long-lived Pod mounting an export ROOT through the in-tree
// kubernetes.io/nfs plugin, bypassing csi-nfs, so a scenario can assert what the
// driver did to the share. Plain exports only: an in-tree mount carries no xprtsec,
// which a TLS-only export refuses.
type Inspector struct {
	name      string
	namespace string
	server    Server
	restCfg   *rest.Config
	cl        client.Client
}

const InspectorMountPath = "/export"

func NewInspector(ctx context.Context, cl client.Client, restCfg *rest.Config, namespace string, srv Server, image string, bindTimeout, readyTimeout time.Duration) (*Inspector, error) {
	if !srv.Plain() {
		return nil, fmt.Errorf("inspector supports plain exports only, got %s", srv)
	}
	name := "e2e-inspect-" + srv.Key
	empty := ""

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			// Nothing on the server belongs to this PV, so reclaim must not touch it.
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              empty,
			// Pinned: each host serves one version, and the in-tree plugin would
			// otherwise negotiate the client default.
			MountOptions: []string{"nfsvers=" + srv.NFSVersion},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{Server: srv.Host, Path: srv.Share},
			},
			ClaimRef: &corev1.ObjectReference{Namespace: namespace, Name: name},
		},
	}
	if err := cl.Create(ctx, pv); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create inspector PV %s: %w", name, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &empty,
			VolumeName:       name,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	if err := cl.Create(ctx, pvc); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create inspector PVC %s: %w", name, err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"app": "csi-nfs-e2e-inspector"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:         ProbeContainerName,
				Image:        image,
				Command:      []string{"sh", "-c", "sleep 360000"},
				VolumeMounts: []corev1.VolumeMount{{Name: "export", MountPath: InspectorMountPath}},
			}},
			Volumes: []corev1.Volume{{
				Name: "export",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name},
				},
			}},
		},
	}
	if err := cl.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create inspector Pod %s: %w", name, err)
	}

	if err := WaitPVCBound(ctx, cl, namespace, name, bindTimeout); err != nil {
		return nil, fmt.Errorf("inspector for %s: %w", srv, err)
	}
	if err := WaitPodReady(ctx, cl, namespace, name, readyTimeout); err != nil {
		return nil, fmt.Errorf("inspector for %s: %w", srv, err)
	}

	return &Inspector{name: name, namespace: namespace, server: srv, restCfg: restCfg, cl: cl}, nil
}

func (i *Inspector) Close(ctx context.Context, timeout time.Duration) error {
	var errs []string
	if err := DeletePodWait(ctx, i.cl, i.namespace, i.name, timeout); err != nil {
		errs = append(errs, err.Error())
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: i.namespace, Name: i.name}}
	if err := i.cl.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("delete inspector PVC %s: %v", i.name, err))
	}
	if err := DeletePVIfExists(ctx, i.cl, i.name); err != nil {
		errs = append(errs, fmt.Sprintf("delete inspector PV %s: %v", i.name, err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("close inspector: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (i *Inspector) Exec(ctx context.Context, script string) (string, error) {
	stdout, stderr, err := storagekube.ExecInPod(ctx, i.restCfg, i.namespace, i.name, ProbeContainerName,
		[]string{"sh", "-c", script})
	if err != nil {
		return stdout, fmt.Errorf("inspector exec on %s: %w (stderr=%q)", i.server, err, stderr)
	}
	return stdout, nil
}

// List names the entries directly under the export root.
func (i *Inspector) List(ctx context.Context) ([]string, error) {
	out, err := i.Exec(ctx, "ls -1A "+InspectorMountPath+" 2>/dev/null || true")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (i *Inspector) Exists(ctx context.Context, rel string) (bool, error) {
	out, err := i.Exec(ctx, fmt.Sprintf("[ -e %s/%s ] && echo yes || echo no", InspectorMountPath, rel))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "yes", nil
}

// Mode returns octal permission bits.
func (i *Inspector) Mode(ctx context.Context, rel string) (string, error) {
	out, err := i.Exec(ctx, fmt.Sprintf("stat -c %%a %s/%s", InspectorMountPath, rel))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (i *Inspector) ReadFile(ctx context.Context, rel string) (string, error) {
	out, err := i.Exec(ctx, fmt.Sprintf("cat %s/%s", InspectorMountPath, rel))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// RemoveAll cleans up after scenarios that leave data behind.
func (i *Inspector) RemoveAll(ctx context.Context, rel string) error {
	if strings.TrimSpace(rel) == "" || strings.Contains(rel, "..") {
		return fmt.Errorf("refusing to remove suspicious path %q from %s", rel, i.server)
	}
	_, err := i.Exec(ctx, fmt.Sprintf("rm -rf %s/%s", InspectorMountPath, rel))
	return err
}
