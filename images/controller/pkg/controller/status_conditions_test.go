/*
Copyright 2025 Flant JSC

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
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/conditions"
)

const testNSCName = "nsc-1"

func newStatusTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()

	scheme := apiruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.NFSStorageClass{}).
		WithObjects(objs...).
		Build()
}

func readNSC(t *testing.T, cl client.Client) *v1alpha1.NFSStorageClass {
	t.Helper()
	got := &v1alpha1.NFSStorageClass{}
	if err := cl.Get(context.Background(), client.ObjectKey{Name: testNSCName}, got); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	return got
}

// NFSStorageClass declares the condition types the module controller publishes;
// this checks that the writer delivers exactly those. RootSquash is declared for
// the root-squash probe of the CSI controller Pod and is held to it there.
//
// A condition type that is declared but never written is worse than one that
// does not exist: an absent condition is indistinguishable from "not yet
// evaluated", so an operator waits for a verdict that never comes and an alert
// on it never fires. The writer was already covered; what this adds is the link
// to the declared set, which nothing but review enforced. With one condition
// type today the check is cheap; its value is the second type someone adds and
// forgets to write.
//
// Both phases are driven, because a condition written on only one of them would
// leave the other reporting nothing.
func TestEveryDeclaredConditionTypeIsWritten(t *testing.T) {
	for _, tc := range []struct {
		phase  string
		reason string
	}{
		{CreatedStatusPhase, ""},
		{FailedStatusPhase, "the server refused the mount"},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			nsc := &v1alpha1.NFSStorageClass{
				ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 1},
			}
			cl := newStatusTestClient(t, nsc)

			if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, tc.phase, tc.reason); err != nil {
				t.Fatalf("updateNFSStorageClassPhase: %v", err)
			}

			got := readNSC(t, cl)

			present := map[string]bool{}
			for _, c := range got.Status.Conditions {
				present[c.Type] = true

				if c.Status == "" {
					t.Errorf("condition %s needs a status", c.Type)
				}
				if c.Reason == "" {
					t.Errorf("condition %s needs a machine-readable reason", c.Type)
				}
				if c.Message == "" {
					t.Errorf("condition %s needs a message", c.Type)
				}
			}

			for _, condType := range v1alpha1.NFSStorageClassControllerConditionTypes {
				if !present[condType] {
					t.Errorf("NFSStorageClass declares %s but the writer did not set it", condType)
				}
				delete(present, condType)
			}
			for stray := range present {
				t.Errorf("NFSStorageClass writes %s, which it does not declare", stray)
			}
		})
	}
}

func TestUpdateNFSStorageClassPhase_Created(t *testing.T) {
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 3},
	}
	cl := newStatusTestClient(t, nsc)

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, CreatedStatusPhase, ""); err != nil {
		t.Fatalf("updateNFSStorageClassPhase: %v", err)
	}

	got := readNSC(t, cl)
	ready := conditions.Get(got.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition was not published")
	}
	if ready.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %q, want True", ready.Status)
	}
	if ready.Reason != conditions.ReasonReconciled {
		t.Errorf("reason = %q, want %q", ready.Reason, conditions.ReasonReconciled)
	}
	if got.Status.Phase != CreatedStatusPhase {
		t.Errorf("phase = %q, want %q", got.Status.Phase, CreatedStatusPhase)
	}
	if got.Status.Reason != ready.Message {
		t.Errorf("status.reason = %q, want the Ready message %q", got.Status.Reason, ready.Message)
	}
	if got.Status.ObservedGeneration != 3 {
		t.Errorf("observedGeneration = %d, want 3", got.Status.ObservedGeneration)
	}
}

func TestUpdateNFSStorageClassPhase_Failed(t *testing.T) {
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 1},
	}
	cl := newStatusTestClient(t, nsc)

	const reason = "unable to create the StorageClass"
	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, FailedStatusPhase, reason); err != nil {
		t.Fatalf("updateNFSStorageClassPhase: %v", err)
	}

	got := readNSC(t, cl)
	ready := conditions.Get(got.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False, got %+v", ready)
	}
	if ready.Reason != conditions.ReasonReconcileFailed {
		t.Errorf("reason = %q, want %q", ready.Reason, conditions.ReasonReconcileFailed)
	}
	if ready.Message != reason {
		t.Errorf("message = %q, want %q", ready.Message, reason)
	}
	if got.Status.Phase != FailedStatusPhase {
		t.Errorf("phase = %q, want %q", got.Status.Phase, FailedStatusPhase)
	}
	if got.Status.Reason != reason {
		t.Errorf("status.reason = %q, want %q", got.Status.Reason, reason)
	}
}

func TestPhaseFromReady(t *testing.T) {
	for _, tc := range []struct {
		name  string
		conds []metav1.Condition
		want  string
	}{
		{"absent", nil, ""},
		{"true", []metav1.Condition{{Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue}}, CreatedStatusPhase},
		{"false", []metav1.Condition{{Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse}}, FailedStatusPhase},
		{"unknown", []metav1.Condition{{Type: v1alpha1.ConditionTypeReady, Status: metav1.ConditionUnknown}}, ""},
		{"other condition only", []metav1.Condition{{Type: "RootSquash", Status: metav1.ConditionFalse}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := phaseFromReady(tc.conds); got != tc.want {
				t.Errorf("phaseFromReady = %q, want %q", got, tc.want)
			}
		})
	}
}

// The failure paths pass err.Error() through as the condition message, and the
// schema caps it at 32768. Over the cap the API server rejects the whole status
// write, so the resource keeps reporting its previous verdict and the reconcile
// fails on the write instead of on what actually went wrong.
func TestUpdateNFSStorageClassPhase_TruncatesAnOversizedMessage(t *testing.T) {
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 1},
	}
	cl := newStatusTestClient(t, nsc)

	// Multi-byte on purpose. The schema's maxLength is an OpenAPI string
	// length, counted in runes, and TruncateMessage counts the same way — a
	// byte-counting assertion here would fail on a message that is in fact
	// within the limit.
	//
	// Written as an escape rather than the character itself: the module linter
	// rejects non-ASCII bytes in Go sources.
	huge := strings.Repeat("\u044f", conditions.MaxMessageLen+100)
	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, FailedStatusPhase, huge); err != nil {
		t.Fatalf("updateNFSStorageClassPhase: %v", err)
	}

	ready := conditions.Get(readNSC(t, cl).Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition was not published")
	}
	if utf8.RuneCountInString(ready.Message) > conditions.MaxMessageLen {
		t.Errorf("message is %d runes, over the %d the schema allows",
			utf8.RuneCountInString(ready.Message), conditions.MaxMessageLen)
	}
}

func TestUpdateNFSStorageClassPhase_SkipsWriteWhenNothingChanges(t *testing.T) {
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 1},
	}
	cl := newStatusTestClient(t, nsc)

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, CreatedStatusPhase, ""); err != nil {
		t.Fatalf("first update: %v", err)
	}
	first := readNSC(t, cl)

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, CreatedStatusPhase, ""); err != nil {
		t.Fatalf("second update: %v", err)
	}
	second := readNSC(t, cl)

	// A resync that changes nothing must not write: otherwise every requeue
	// produces an etcd write and a watch event for every object.
	if second.ResourceVersion != first.ResourceVersion {
		t.Fatalf("expected no write, resourceVersion moved %s -> %s",
			first.ResourceVersion, second.ResourceVersion)
	}
}

// lastTransitionTime marks when the state changed, not when the controller last
// looked. A pass that only produces a different message must not move it, or
// "Ready=False for longer than 10 minutes" can never fire.
func TestUpdateNFSStorageClassPhase_KeepsTransitionTimeAcrossMessageChange(t *testing.T) {
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: 1},
	}
	cl := newStatusTestClient(t, nsc)

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, FailedStatusPhase, "first failure"); err != nil {
		t.Fatalf("first update: %v", err)
	}
	first := conditions.Get(readNSC(t, cl).Status.Conditions, v1alpha1.ConditionTypeReady)

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, nsc, FailedStatusPhase, "second failure"); err != nil {
		t.Fatalf("second update: %v", err)
	}
	second := conditions.Get(readNSC(t, cl).Status.Conditions, v1alpha1.ConditionTypeReady)

	if second.Message != "second failure" {
		t.Errorf("message was not updated: %q", second.Message)
	}
	if !second.LastTransitionTime.Equal(&first.LastTransitionTime) {
		t.Errorf("lastTransitionTime moved %v -> %v without a status change",
			first.LastTransitionTime, second.LastTransitionTime)
	}
}

// observedGeneration must name the generation that was actually reconciled, so
// a reader can tell "reconciled and healthy" from "has not seen your edit yet".
func TestUpdateNFSStorageClassPhase_ObservedGenerationIsTheReconciledOne(t *testing.T) {
	const (
		reconciledGeneration = 4
		currentGeneration    = 5
	)

	// The fake client does not maintain metadata.generation, so both
	// generations are set explicitly rather than produced by an update.
	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName, Generation: currentGeneration},
	}
	cl := newStatusTestClient(t, nsc)

	reconciled := nsc.DeepCopy()
	reconciled.Generation = reconciledGeneration

	if err := updateNFSStorageClassPhase(context.Background(), cl, nil, reconciled, CreatedStatusPhase, ""); err != nil {
		t.Fatalf("updateNFSStorageClassPhase: %v", err)
	}

	got := readNSC(t, cl)
	if got.Status.ObservedGeneration != reconciledGeneration {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, reconciledGeneration)
	}
	if !conditions.IsStale(got.Status.Conditions, v1alpha1.ConditionTypeReady, currentGeneration) {
		t.Error("the Ready condition should read as stale against the current generation")
	}
}

// drainEvents returns what the fake recorder has received so far.
func drainEvents(rec *events.FakeRecorder) []string {
	var got []string
	for {
		select {
		case e := <-rec.Events:
			got = append(got, e)
		default:
			return got
		}
	}
}

// The status writer records an Event when Ready moves and only then: a pass
// that repeats the verdict, or re-words it, must not add to the resource's
// Events, or the one that tells when it broke scrolls away.
func TestUpdateNFSStorageClassPhase_RecordsReadyTransitions(t *testing.T) {
	const created = "the NFSStorageClass is in the Created phase"

	steps := []struct {
		name   string
		phase  string
		reason string
		want   []string
	}{
		{"first verdict", CreatedStatusPhase, "", []string{"Normal Reconciled " + created}},
		{"same verdict", CreatedStatusPhase, "", nil},
		{"breaks", FailedStatusPhase, "unable to create the StorageClass", []string{"Warning ReconcileFailed unable to create the StorageClass"}},
		{"fails with another message", FailedStatusPhase, "unable to create the StorageClass: timeout", nil},
		{"recovers", CreatedStatusPhase, "", []string{"Normal Reconciled " + created}},
	}

	nsc := &v1alpha1.NFSStorageClass{ObjectMeta: metav1.ObjectMeta{Name: testNSCName}}
	cl := newStatusTestClient(t, nsc)
	rec := events.NewFakeRecorder(10)

	for _, s := range steps {
		if err := updateNFSStorageClassPhase(context.Background(), cl, rec, nsc, s.phase, s.reason); err != nil {
			t.Fatalf("%s: updating status: %v", s.name, err)
		}
		if got := drainEvents(rec); !slices.Equal(got, s.want) {
			t.Errorf("%s: Events = %q, want %q", s.name, got, s.want)
		}
	}
}
