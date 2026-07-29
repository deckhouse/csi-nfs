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
	"io"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DumpDiagnostics never fails: each section degrades to a note about what could not
// be read.
func DumpDiagnostics(ctx context.Context, w io.Writer, cl client.Client, dyn dynamic.Interface, testNamespace string) {
	fmt.Fprintf(w, "\n========== csi-nfs e2e failure diagnostics ==========\n")

	dumpNFSStorageClasses(ctx, w, dyn)
	dumpDerivedStorageClasses(ctx, w, cl)
	dumpPendingVolumes(ctx, w, cl, testNamespace)
	dumpModulePods(ctx, w, cl)
	DumpCSINodeDaemonSet(ctx, w, cl)
	dumpWarningEvents(ctx, w, cl, ModuleNamespace)
	dumpWarningEvents(ctx, w, cl, testNamespace)

	fmt.Fprintf(w, "=====================================================\n")
}

func dumpNFSStorageClasses(ctx context.Context, w io.Writer, dyn dynamic.Interface) {
	fmt.Fprintf(w, "--- NFSStorageClasses ---\n")
	list, err := dyn.Resource(NFSStorageClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(w, "  <error listing: %v>\n", err)
		return
	}
	if len(list.Items) == 0 {
		fmt.Fprintf(w, "  <none>\n")
		return
	}
	for i := range list.Items {
		item := &list.Items[i]
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		reason, _, _ := unstructured.NestedString(item.Object, "status", "reason")
		host, _, _ := unstructured.NestedString(item.Object, "spec", "connection", "host")
		share, _, _ := unstructured.NestedString(item.Object, "spec", "connection", "share")
		version, _, _ := unstructured.NestedString(item.Object, "spec", "connection", "nfsVersion")
		fmt.Fprintf(w, "  %s: phase=%q reason=%q target=%s:%s v%s\n",
			item.GetName(), phase, reason, host, share, version)
	}
}

func dumpDerivedStorageClasses(ctx context.Context, w io.Writer, cl client.Client) {
	fmt.Fprintf(w, "--- StorageClasses with provisioner %s ---\n", CSIDriverName)
	var list storagev1.StorageClassList
	if err := cl.List(ctx, &list); err != nil {
		fmt.Fprintf(w, "  <error listing: %v>\n", err)
		return
	}
	count := 0
	for i := range list.Items {
		sc := &list.Items[i]
		if sc.Provisioner != CSIDriverName {
			continue
		}
		count++
		fmt.Fprintf(w, "  %s: mountOptions=%v params=%v\n", sc.Name, sc.MountOptions, sc.Parameters)
	}
	if count == 0 {
		fmt.Fprintf(w, "  <none>\n")
	}
}

func dumpPendingVolumes(ctx context.Context, w io.Writer, cl client.Client, namespace string) {
	fmt.Fprintf(w, "--- non-Bound PVCs in %s ---\n", namespace)
	var pvcs corev1.PersistentVolumeClaimList
	if err := cl.List(ctx, &pvcs, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(w, "  <error listing: %v>\n", err)
		return
	}
	count := 0
	for i := range pvcs.Items {
		p := &pvcs.Items[i]
		if p.Status.Phase == corev1.ClaimBound {
			continue
		}
		count++
		sc := ""
		if p.Spec.StorageClassName != nil {
			sc = *p.Spec.StorageClassName
		}
		fmt.Fprintf(w, "  %s: phase=%s sc=%s\n", p.Name, p.Status.Phase, sc)
	}
	if count == 0 {
		fmt.Fprintf(w, "  <none>\n")
	}
}

func dumpModulePods(ctx context.Context, w io.Writer, cl client.Client) {
	fmt.Fprintf(w, "--- pods in %s ---\n", ModuleNamespace)
	var pods corev1.PodList
	if err := cl.List(ctx, &pods, client.InNamespace(ModuleNamespace)); err != nil {
		fmt.Fprintf(w, "  <error listing pods: %v>\n", err)
		return
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		fmt.Fprintf(w, "  %s (node %s): phase=%s ready=%v\n", p.Name, p.Spec.NodeName, p.Status.Phase, IsPodReady(p))
	}
}

// Per-container, because a stalled rollout is usually one init container failing on
// one node (wait-rpcbind, or net-handshake-checker on a kernel without
// CONFIG_NET_HANDSHAKE) and the DaemonSet counters name neither.
func DumpCSINodeDaemonSet(ctx context.Context, w io.Writer, cl client.Client) {
	fmt.Fprintf(w, "\n--- DaemonSet %s/%s ---\n", ModuleNamespace, CSINodeDaemonSetName)

	var ds appsv1.DaemonSet
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ModuleNamespace, Name: CSINodeDaemonSetName}, &ds); err != nil {
		fmt.Fprintf(w, "  <error getting DaemonSet: %v>\n", err)
	} else {
		fmt.Fprintf(w, "  desired=%d current=%d ready=%d updated=%d available=%d unavailable=%d\n",
			ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady,
			ds.Status.UpdatedNumberScheduled, ds.Status.NumberAvailable, ds.Status.NumberUnavailable)
		var initNames, names []string
		for _, c := range ds.Spec.Template.Spec.InitContainers {
			initNames = append(initNames, c.Name)
		}
		for _, c := range ds.Spec.Template.Spec.Containers {
			names = append(names, c.Name)
		}
		fmt.Fprintf(w, "  initContainers=%v containers=%v\n", initNames, names)
	}

	var pods corev1.PodList
	if err := cl.List(ctx, &pods, client.InNamespace(ModuleNamespace)); err != nil {
		fmt.Fprintf(w, "  <error listing pods: %v>\n", err)
		return
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if !strings.HasPrefix(p.Name, CSINodeDaemonSetName) {
			continue
		}
		fmt.Fprintf(w, "  %s (node %s): phase=%s ready=%v\n", p.Name, p.Spec.NodeName, p.Status.Phase, IsPodReady(p))
		dumpContainerStatuses(w, " init", p.Status.InitContainerStatuses)
		dumpContainerStatuses(w, "     ", p.Status.ContainerStatuses)
	}
	fmt.Fprintf(w, "\n")
}

func dumpContainerStatuses(w io.Writer, prefix string, statuses []corev1.ContainerStatus) {
	for i := range statuses {
		cs := &statuses[i]
		state := "running"
		switch {
		case cs.State.Waiting != nil:
			state = fmt.Sprintf("waiting(%s): %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		case cs.State.Terminated != nil:
			state = fmt.Sprintf("terminated(%s, exit=%d): %s",
				cs.State.Terminated.Reason, cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
		}
		fmt.Fprintf(w, "   %s %-24s ready=%-5v restarts=%d %s\n",
			prefix, cs.Name, cs.Ready, cs.RestartCount, state)
	}
}

func dumpWarningEvents(ctx context.Context, w io.Writer, cl client.Client, namespace string) {
	fmt.Fprintf(w, "--- recent Warning events in %s ---\n", namespace)
	var events corev1.EventList
	if err := cl.List(ctx, &events, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(w, "  <error listing events: %v>\n", err)
		return
	}
	count := 0
	for i := range events.Items {
		e := &events.Items[i]
		if e.Type != corev1.EventTypeWarning {
			continue
		}
		fmt.Fprintf(w, "  %s/%s: %s: %s\n", e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Reason, e.Message)
		count++
		if count >= 30 {
			fmt.Fprintf(w, "  ... (truncated)\n")
			break
		}
	}
	if count == 0 {
		fmt.Fprintf(w, "  <none>\n")
	}
}
