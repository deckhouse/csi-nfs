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

// Package controller publishes the RootSquash condition of NFSStorageClasses.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/root-squash-probe/pkg/probe"
	"github.com/deckhouse/sds-common-lib/conditions"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	Name = "root-squash-probe"

	// probeFilePrefix starts the name of the probe file in the export root.
	// Hidden, and followed by a hash of the class name rather than the name
	// itself, which may be longer than a file name may be.
	probeFilePrefix = ".csi-nfs-root-squash-probe-"
)

// Prober checks one export.
type Prober interface {
	Probe(ctx context.Context, t probe.Target) probe.Result
}

// Reconciler probes the export of every NFSStorageClass when the class
// appears or its spec changes, and every Interval after that.
//
// It writes only the RootSquash condition. The module controller writes the
// rest of the status, Ready included, and both writes go through optimistic
// concurrency on a freshly read object, so neither can drop the condition of
// the other: a write based on an object that misses the other's latest change
// is refused with a conflict and redone.
type Reconciler struct {
	// Client reads from the cache and writes the status.
	Client client.Client
	// APIReader reads the object the status write is based on.
	APIReader client.Reader
	Recorder  record.EventRecorder
	Prober    Prober
	Interval  time.Duration
	Log       logger.Logger
	// Now is time.Now outside tests.
	Now func() time.Time

	mu sync.Mutex
	// checked holds the last published probe of each class. It is only kept
	// in memory: after a restart or a change of leader every class is probed
	// once more, which costs a mount per class and nothing else.
	checked map[string]check
}

type check struct {
	uid        types.UID
	generation int64
	at         time.Time
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, concurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(Name).
		// Status writes, the probe's own included, do not trigger a probe.
		For(&v1alpha1.NFSStorageClass{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}

// Reconcile returns an error only when the API server does. An export that
// cannot be probed is a verdict of its own, Unknown, and is probed again after
// Interval like any other rather than retried with a backoff.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.Log.With("nfsStorageClass", req.Name)

	nsc := &v1alpha1.NFSStorageClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, nsc); err != nil {
		if apierrors.IsNotFound(err) {
			r.forget(req.Name)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if nsc.DeletionTimestamp != nil || nsc.Spec.Connection == nil {
		r.forget(req.Name)
		return reconcile.Result{}, nil
	}

	if wait := r.untilDue(nsc); wait > 0 {
		return reconcile.Result{RequeueAfter: wait}, nil
	}

	res := r.Prober.Probe(ctx, TargetFor(nsc))
	log.Info("probed the export", "status", string(res.Status), "reason", res.Reason, "message", res.Message)

	before, err := r.publish(ctx, nsc, res)
	if err != nil {
		return reconcile.Result{}, err
	}
	r.remember(nsc)

	if res.Status == metav1.ConditionTrue && (before == nil || before.Status != metav1.ConditionTrue) {
		r.Recorder.Event(nsc, corev1.EventTypeWarning, res.Reason, res.Message)
	}

	return reconcile.Result{RequeueAfter: r.Interval}, nil
}

// TargetFor is the export of nsc.
func TargetFor(nsc *v1alpha1.NFSStorageClass) probe.Target {
	sum := sha256.Sum256([]byte(nsc.Name))
	return probe.Target{
		Host:       nsc.Spec.Connection.Host,
		Share:      nsc.Spec.Connection.Share,
		NFSVersion: nsc.Spec.Connection.NFSVersion,
		TLS:        nsc.Spec.Connection.Tls,
		MTLS:       nsc.Spec.Connection.Mtls,
		FileName:   probeFilePrefix + hex.EncodeToString(sum[:8]),
	}
}

// publish sets the RootSquash condition and returns the one it replaced.
func (r *Reconciler) publish(ctx context.Context, nsc *v1alpha1.NFSStorageClass, res probe.Result) (*metav1.Condition, error) {
	key := client.ObjectKeyFromObject(nsc)
	cond := metav1.Condition{
		Type:               v1alpha1.ConditionTypeRootSquash,
		Status:             res.Status,
		Reason:             res.Reason,
		Message:            conditions.TruncateMessage(res.Message),
		ObservedGeneration: nsc.Generation,
	}

	var before *metav1.Condition
	err := conditions.UpdateStatusVia(ctx,
		func(ctx context.Context) (*v1alpha1.NFSStorageClass, error) {
			fresh := &v1alpha1.NFSStorageClass{}
			if err := r.APIReader.Get(ctx, key, fresh); err != nil {
				return nil, err
			}
			return fresh, nil
		},
		func(ctx context.Context, fresh *v1alpha1.NFSStorageClass) error {
			return r.Client.Status().Update(ctx, fresh)
		},
		func(fresh *v1alpha1.NFSStorageClass) {
			before = nil
			// A class deleted and created again under the same name while
			// the probe ran: the verdict is about the export of the old one.
			if fresh.UID != nsc.UID {
				return
			}
			if fresh.Status == nil {
				fresh.Status = &v1alpha1.NFSStorageClassStatus{}
			}
			if prev := conditions.Get(fresh.Status.Conditions, v1alpha1.ConditionTypeRootSquash); prev != nil {
				c := *prev
				before = &c
			}
			conditions.Set(&fresh.Status.Conditions, cond)
		})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	return before, err
}

func (r *Reconciler) untilDue(nsc *v1alpha1.NFSStorageClass) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.checked[nsc.Name]
	if !ok || c.uid != nsc.UID || c.generation != nsc.Generation {
		return 0
	}
	return r.Interval - r.Now().Sub(c.at)
}

func (r *Reconciler) remember(nsc *v1alpha1.NFSStorageClass) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checked == nil {
		r.checked = map[string]check{}
	}
	r.checked[nsc.Name] = check{uid: nsc.UID, generation: nsc.Generation, at: r.Now()}
}

func (r *Reconciler) forget(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checked, name)
}
