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

package v1alpha1

// Condition types published in status.conditions.
//
// These follow the Kubernetes API conventions for typical status properties:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
const (
	// ConditionTypeReady reports whether the controller has fully reconciled
	// the resource:
	//
	//   - True    the StorageClass, secret and VolumeSnapshotClass are in place;
	//   - False   a reconcile pass failed, see the condition message;
	//   - Unknown the controller has not observed the resource yet.
	//
	// It matches the aggregate Ready condition used across the storage
	// modules; the shared helpers live in
	// github.com/deckhouse/sds-common-lib/conditions.
	ConditionTypeReady = "Ready"
)

// NFSStorageClassConditionTypes is every condition type an NFSStorageClass
// publishes.
//
// A condition type that is declared but never written is worse than one that
// does not exist: an absent condition is indistinguishable from "not yet
// evaluated", so an operator waits for a verdict that never comes and an alert
// on it never fires. Keeping the set in one list is what lets a test hold the
// controller to it.
var NFSStorageClassConditionTypes = []string{
	ConditionTypeReady,
}
