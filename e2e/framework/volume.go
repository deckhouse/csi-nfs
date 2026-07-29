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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	storagekube "github.com/deckhouse/storage-e2e/pkg/kubernetes"
)

const (
	ProbeContainerName = "probe"
	ProbeMountPath     = "/data"
	ProbeFilePath      = "/data/probe.txt"
)

var VolumeSnapshotGVR = schema.GroupVersionResource{
	Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots",
}

func BuildPVC(namespace, name, sc, size string, mode corev1.PersistentVolumeAccessMode, dataSource *corev1.TypedLocalObjectReference) *corev1.PersistentVolumeClaim {
	scp := sc
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{mode},
			StorageClassName: &scp,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
		},
	}
	if dataSource != nil {
		pvc.Spec.DataSource = dataSource
	}
	return pvc
}

type ProbePodSpec struct {
	Namespace string
	Name      string
	PVCName   string
	Image     string
	Marker    string
	Write     bool // write Marker at boot; readers leave it false
	// Applied as nodeSelector, NOT spec.nodeName: under WaitForFirstConsumer a Pod
	// with spec.nodeName skips the scheduler, so its PVC never gets the
	// selected-node annotation and is never provisioned.
	Node string
}

func BuildProbePod(spec ProbePodSpec) *corev1.Pod {
	script := `sleep 360000`
	if spec.Write {
		script = fmt.Sprintf(`echo -n "$MARKER" > %s && sync && cat %s && sleep 360000`, ProbeFilePath, ProbeFilePath)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: spec.Namespace,
			Name:      spec.Name,
			Labels:    map[string]string{"app": "csi-nfs-e2e"},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:         ProbeContainerName,
				Image:        spec.Image,
				Command:      []string{"sh", "-c", script},
				Env:          []corev1.EnvVar{{Name: "MARKER", Value: spec.Marker}},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: ProbeMountPath}},
			}},
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: spec.PVCName},
				},
			}},
		},
	}
	if spec.Node != "" {
		pod.Spec.NodeSelector = map[string]string{corev1.LabelHostname: spec.Node}
	}
	return pod
}

func SnapshotDataSource(name string) *corev1.TypedLocalObjectReference {
	group := "snapshot.storage.k8s.io"
	return &corev1.TypedLocalObjectReference{APIGroup: &group, Kind: "VolumeSnapshot", Name: name}
}

func PVCDataSource(name string) *corev1.TypedLocalObjectReference {
	return &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: name}
}

func WaitPVCBound(ctx context.Context, cl client.Client, namespace, name string, timeout time.Duration) error {
	var last error
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var pvc corev1.PersistentVolumeClaim
		last = cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pvc)
		return last == nil && pvc.Status.Phase == corev1.ClaimBound, nil
	}); err != nil {
		return fmt.Errorf("PVC %s/%s did not bind: %w (last get: %v)", namespace, name, err, last)
	}
	return nil
}

func WaitPodReady(ctx context.Context, cl client.Client, namespace, name string, timeout time.Duration) error {
	var last error
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		last = cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pod)
		return last == nil && IsPodReady(&pod), nil
	}); err != nil {
		return fmt.Errorf("Pod %s/%s did not become Ready: %w (last get: %v)", namespace, name, err, last)
	}
	return nil
}

func WaitPodGone(ctx context.Context, cl client.Client, namespace, name string, timeout time.Duration) error {
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pod)
		return apierrors.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("Pod %s/%s did not disappear: %w", namespace, name, err)
	}
	return nil
}

func IsPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func VerifyProbeFile(ctx context.Context, restCfg *rest.Config, namespace, podName, want string) error {
	out, err := storagekube.ReadFileFromPod(ctx, restCfg, namespace, podName, ProbeContainerName, ProbeFilePath)
	if err != nil {
		return fmt.Errorf("read %s in %s/%s: %w", ProbeFilePath, namespace, podName, err)
	}
	if strings.TrimSpace(out) != want {
		return fmt.Errorf("probe mismatch in %s/%s: want %q, got %q", namespace, podName, want, strings.TrimSpace(out))
	}
	return nil
}

func ApplyPVCAndPod(ctx context.Context, cl client.Client, pvc *corev1.PersistentVolumeClaim, pod *corev1.Pod, bindTimeout, readyTimeout time.Duration) error {
	if err := cl.Create(ctx, pvc); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create PVC %s/%s: %w", pvc.Namespace, pvc.Name, err)
	}
	if err := cl.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	if err := WaitPVCBound(ctx, cl, pvc.Namespace, pvc.Name, bindTimeout); err != nil {
		return err
	}
	return WaitPodReady(ctx, cl, pod.Namespace, pod.Name, readyTimeout)
}

func ApplyPod(ctx context.Context, cl client.Client, pod *corev1.Pod, readyTimeout time.Duration) error {
	if err := cl.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return WaitPodReady(ctx, cl, pod.Namespace, pod.Name, readyTimeout)
}

func PodNodeName(ctx context.Context, cl client.Client, namespace, name string) (string, error) {
	var pod corev1.Pod
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pod); err != nil {
		return "", err
	}
	return pod.Spec.NodeName, nil
}

// The PV name is also the directory csi-nfs creates on the share.
func PVName(ctx context.Context, cl client.Client, namespace, pvcName string) (string, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: pvcName}, &pvc); err != nil {
		return "", err
	}
	if pvc.Spec.VolumeName == "" {
		return "", fmt.Errorf("PVC %s/%s is not bound yet", namespace, pvcName)
	}
	return pvc.Spec.VolumeName, nil
}

func GetPV(ctx context.Context, cl client.Client, name string) (*corev1.PersistentVolume, error) {
	var pv corev1.PersistentVolume
	if err := cl.Get(ctx, client.ObjectKey{Name: name}, &pv); err != nil {
		return nil, err
	}
	return &pv, nil
}

func DeletePVIfExists(ctx context.Context, cl client.Client, name string) error {
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := cl.Delete(ctx, pv); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// Waits for status.capacity to follow the raised request.
func ResizePVC(ctx context.Context, cl client.Client, namespace, name, newSize string, timeout time.Duration) error {
	want := resource.MustParse(newSize)

	var pvc corev1.PersistentVolumeClaim
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pvc); err != nil {
		return fmt.Errorf("get PVC %s/%s: %w", namespace, name, err)
	}
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = want
	if err := cl.Update(ctx, &pvc); err != nil {
		return fmt.Errorf("resize PVC %s/%s: %w", namespace, name, err)
	}

	var lastCapacity corev1.ResourceList
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var cur corev1.PersistentVolumeClaim
		if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &cur); err != nil {
			return false, nil
		}
		lastCapacity = cur.Status.Capacity
		capacity, ok := cur.Status.Capacity[corev1.ResourceStorage]
		return ok && capacity.Cmp(want) >= 0, nil
	}); err != nil {
		return fmt.Errorf("PVC %s/%s did not reach %s: %w (status.capacity=%v)", namespace, name, newSize, err, lastCapacity)
	}
	return nil
}

// A missing Pod is success, so this doubles as cleanup.
func DeletePodWait(ctx context.Context, cl client.Client, namespace, name string, timeout time.Duration) error {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := cl.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Pod %s/%s: %w", namespace, name, err)
	}
	return WaitPodGone(ctx, cl, namespace, name, timeout)
}

// Under the default Delete policy, the claim disappearing proves csi-nfs called
// DeleteVolume.
func DeletePVCWait(ctx context.Context, cl client.Client, namespace, name string, timeout time.Duration) error {
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := cl.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete PVC %s/%s: %w", namespace, name, err)
	}
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var cur corev1.PersistentVolumeClaim
		err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &cur)
		return apierrors.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("PVC %s/%s was not deleted: %w", namespace, name, err)
	}
	return nil
}

// csi-nfs tars the volume directory into the root of the share with no atomicity,
// so callers stop the workload first.
func CreateSnapshot(ctx context.Context, dyn dynamic.Interface, namespace, name, srcPVC, snapClass string, timeout time.Duration) error {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(schema.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"})
	snap.SetNamespace(namespace)
	snap.SetName(name)
	snap.Object["spec"] = map[string]interface{}{
		"volumeSnapshotClassName": snapClass,
		"source":                  map[string]interface{}{"persistentVolumeClaimName": srcPVC},
	}
	if _, err := dyn.Resource(VolumeSnapshotGVR).Namespace(namespace).Create(ctx, snap, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create VolumeSnapshot %s/%s: %w", namespace, name, err)
	}

	var lastErr string
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		obj, err := dyn.Resource(VolumeSnapshotGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if msg, ok, _ := unstructured.NestedString(obj.Object, "status", "error", "message"); ok && msg != "" {
			lastErr = msg
		}
		ready, found, _ := unstructured.NestedBool(obj.Object, "status", "readyToUse")
		return found && ready, nil
	}); err != nil {
		return fmt.Errorf("VolumeSnapshot %s/%s did not become readyToUse: %w (last status error: %s)", namespace, name, err, lastErr)
	}
	return nil
}

func DeleteSnapshotWait(ctx context.Context, dyn dynamic.Interface, namespace, name string, timeout time.Duration) error {
	err := dyn.Resource(VolumeSnapshotGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete VolumeSnapshot %s/%s: %w", namespace, name, err)
	}
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		_, err := dyn.Resource(VolumeSnapshotGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("VolumeSnapshot %s/%s was not deleted: %w", namespace, name, err)
	}
	return nil
}
