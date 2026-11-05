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

package controller

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/root-squash-probe/pkg/probe"
	"github.com/deckhouse/sds-common-lib/conditions"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	testNSCName  = "test-nsc"
	testInterval = 10 * time.Minute
)

var (
	squashed    = probe.Result{Status: metav1.ConditionTrue, Reason: v1alpha1.ReasonRootSquashed, Message: "root is squashed"}
	notSquashed = probe.Result{Status: metav1.ConditionFalse, Reason: v1alpha1.ReasonRootNotSquashed, Message: "root is root"}
	unreachable = probe.Result{Status: metav1.ConditionUnknown, Reason: v1alpha1.ReasonMountFailed, Message: "no route to host"}
)

type fakeProber struct {
	result  probe.Result
	targets []probe.Target
}

func (p *fakeProber) Probe(_ context.Context, t probe.Target) probe.Result {
	p.targets = append(p.targets, t)
	return p.result
}

type harness struct {
	cl       client.Client
	prober   *fakeProber
	recorder *record.FakeRecorder
	now      time.Time
	r        *Reconciler
}

func newHarness(t *testing.T, objs ...client.Object) *harness {
	t.Helper()
	scheme := apiruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.NFSStorageClass{}).
		Build()

	h := &harness{
		cl:       cl,
		prober:   &fakeProber{result: notSquashed},
		recorder: record.NewFakeRecorder(10),
		now:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	h.r = &Reconciler{
		Client:    cl,
		APIReader: cl,
		Recorder:  h.recorder,
		Prober:    h.prober,
		Interval:  testInterval,
		Log:       logger.NewNop(),
		Now:       func() time.Time { return h.now },
	}
	return h
}

func (h *harness) reconcile(t *testing.T) reconcile.Result {
	t.Helper()
	res, err := h.r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: testNSCName}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return res
}

func (h *harness) get(t *testing.T) *v1alpha1.NFSStorageClass {
	t.Helper()
	nsc := &v1alpha1.NFSStorageClass{}
	if err := h.cl.Get(context.Background(), client.ObjectKey{Name: testNSCName}, nsc); err != nil {
		t.Fatal(err)
	}
	return nsc
}

func (h *harness) events() []string {
	var out []string
	for {
		select {
		case e := <-h.recorder.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func newNSC() *v1alpha1.NFSStorageClass {
	return &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, UID: "uid-1", Generation: 1},
		Spec: v1alpha1.NFSStorageClassSpec{
			Connection: &v1alpha1.NFSStorageClassConnection{
				Host: "nfs.example.com", Share: "/srv/share", NFSVersion: "4.1", Tls: true, Mtls: true,
			},
			ReclaimPolicy:     "Delete",
			VolumeBindingMode: "WaitForFirstConsumer",
		},
	}
}

func TestReconcilePublishesTheVerdict(t *testing.T) {
	h := newHarness(t, newNSC())

	res := h.reconcile(t)

	if res.RequeueAfter != testInterval {
		t.Errorf("RequeueAfter = %s, want %s", res.RequeueAfter, testInterval)
	}
	cond := conditions.Get(h.get(t).Status.Conditions, v1alpha1.ConditionTypeRootSquash)
	if cond == nil {
		t.Fatal("RootSquash was not published")
	}
	if cond.Status != metav1.ConditionFalse || cond.Reason != v1alpha1.ReasonRootNotSquashed || cond.Message != notSquashed.Message {
		t.Errorf("RootSquash = %+v", cond)
	}
	if cond.ObservedGeneration != 1 {
		t.Errorf("observedGeneration = %d, want 1", cond.ObservedGeneration)
	}

	want := probe.Target{
		Host: "nfs.example.com", Share: "/srv/share", NFSVersion: "4.1", TLS: true, MTLS: true,
		FileName: TargetFor(newNSC()).FileName,
	}
	if !slices.Equal(h.prober.targets, []probe.Target{want}) {
		t.Errorf("probed %+v, want %+v", h.prober.targets, want)
	}
}

// The probe writes RootSquash and nothing else: Ready and the rest of the
// status belong to the module controller.
func TestReconcileLeavesTheRestOfTheStatusAlone(t *testing.T) {
	nsc := newNSC()
	ready := metav1.Condition{
		Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue, Reason: conditions.ReasonReconciled,
		Message: "the NFSStorageClass is in the Created phase", ObservedGeneration: 1,
		LastTransitionTime: metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
	nsc.Status = &v1alpha1.NFSStorageClassStatus{
		Phase: "Created", ObservedGeneration: 1, Conditions: []metav1.Condition{ready},
	}
	h := newHarness(t, nsc)
	h.prober.result = squashed

	h.reconcile(t)

	got := h.get(t).Status
	if got.Phase != "Created" || got.ObservedGeneration != 1 {
		t.Errorf("status = %+v, the phase and observedGeneration changed", got)
	}
	gotReady := conditions.Get(got.Conditions, v1alpha1.ConditionTypeReady)
	if gotReady == nil || !gotReady.LastTransitionTime.Equal(&ready.LastTransitionTime) || gotReady.Status != ready.Status ||
		gotReady.Message != ready.Message {
		t.Errorf("Ready = %+v, want it untouched: %+v", gotReady, ready)
	}

	var written []string
	for _, c := range got.Conditions {
		if c.Type != v1alpha1.ConditionTypeReady {
			written = append(written, c.Type)
		}
	}
	if !slices.Equal(written, v1alpha1.NFSStorageClassProbeConditionTypes) {
		t.Errorf("the probe wrote %v, it declares %v", written, v1alpha1.NFSStorageClassProbeConditionTypes)
	}
}

// Every declared condition type has exactly one writer: the module
// controller or this probe.
func TestConditionTypesHaveOneWriterEach(t *testing.T) {
	writers := append(slices.Clone(v1alpha1.NFSStorageClassControllerConditionTypes), v1alpha1.NFSStorageClassProbeConditionTypes...)
	slices.Sort(writers)
	declared := slices.Sorted(slices.Values(v1alpha1.NFSStorageClassConditionTypes))
	if !slices.Equal(writers, declared) {
		t.Errorf("the writers cover %v, NFSStorageClass declares %v", writers, declared)
	}
}

func TestReconcileProbesOncePerInterval(t *testing.T) {
	h := newHarness(t, newNSC())

	h.reconcile(t)
	h.now = h.now.Add(4 * time.Minute)
	res := h.reconcile(t)

	if len(h.prober.targets) != 1 {
		t.Fatalf("probed %d times within the interval, want once", len(h.prober.targets))
	}
	if res.RequeueAfter != 6*time.Minute {
		t.Errorf("RequeueAfter = %s, want the rest of the interval", res.RequeueAfter)
	}

	h.now = h.now.Add(6 * time.Minute)
	h.reconcile(t)
	if len(h.prober.targets) != 2 {
		t.Errorf("probed %d times after the interval, want twice", len(h.prober.targets))
	}
}

func TestReconcileProbesASpecChangeAtOnce(t *testing.T) {
	h := newHarness(t, newNSC())
	h.reconcile(t)

	nsc := h.get(t)
	nsc.Spec.ChmodPermissions = "0777"
	nsc.Generation = 2
	if err := h.cl.Update(context.Background(), nsc); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t)

	if len(h.prober.targets) != 2 {
		t.Errorf("probed %d times, want a second probe for the new generation", len(h.prober.targets))
	}
}

// An export that cannot be probed is a verdict too: no error, so no backoff
// loop of mounts, just the next probe after the interval.
func TestReconcileUnknownIsNotAnError(t *testing.T) {
	h := newHarness(t, newNSC())
	h.prober.result = unreachable

	res := h.reconcile(t)

	if res.RequeueAfter != testInterval {
		t.Errorf("RequeueAfter = %s, want %s", res.RequeueAfter, testInterval)
	}
	cond := conditions.Get(h.get(t).Status.Conditions, v1alpha1.ConditionTypeRootSquash)
	if cond == nil || cond.Status != metav1.ConditionUnknown || cond.Reason != v1alpha1.ReasonMountFailed {
		t.Errorf("RootSquash = %+v, want Unknown/MountFailed", cond)
	}
}

func TestReconcileWarnsWhenRootBecomesSquashed(t *testing.T) {
	h := newHarness(t, newNSC())
	step := func(res probe.Result) []string {
		h.prober.result = res
		h.now = h.now.Add(testInterval)
		h.reconcile(t)
		return h.events()
	}

	if ev := step(squashed); len(ev) != 1 || !strings.HasPrefix(ev[0], "Warning "+v1alpha1.ReasonRootSquashed) {
		t.Errorf("events after the first squashed verdict = %v, want one warning", ev)
	}
	if ev := step(squashed); len(ev) != 0 {
		t.Errorf("events for a verdict that did not change = %v, want none", ev)
	}
	if ev := step(notSquashed); len(ev) != 0 {
		t.Errorf("events for no squashing = %v, want none", ev)
	}
	if ev := step(squashed); len(ev) != 1 {
		t.Errorf("events after squashing came back = %v, want one warning", ev)
	}
}

func TestReconcileSkips(t *testing.T) {
	deleting := newNSC()
	deleting.Finalizers = []string{"storage.deckhouse.io/nfs-storage-class-controller"}
	deleting.DeletionTimestamp = &metav1.Time{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	for name, objs := range map[string][]client.Object{
		"absent":   nil,
		"deleting": {deleting},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, objs...)

			res := h.reconcile(t)

			if len(h.prober.targets) != 0 {
				t.Errorf("probed %v", h.prober.targets)
			}
			if res.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %s, want none", res.RequeueAfter)
			}
		})
	}
}

func TestTargetForFileName(t *testing.T) {
	long := newNSC()
	long.Name = strings.Repeat("a", 253)

	name := TargetFor(long).FileName
	if !strings.HasPrefix(name, ".") || len(name) > 255 {
		t.Errorf("file name %q must be hidden and fit a file name", name)
	}
	if name != TargetFor(long).FileName {
		t.Error("the file name of a class must be stable between probes")
	}
	if name == TargetFor(newNSC()).FileName {
		t.Error("two classes must not share a probe file")
	}
}
