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

	// ConditionTypeRootSquash reports whether the NFS server maps root of the
	// cluster nodes to another user on the export of the class (root_squash or
	// all_squash):
	//
	//   - True    root is squashed: a file root creates in the export root
	//             cannot be handed to root, or root may not create one at all;
	//   - False   root keeps its identity on the export;
	//   - Unknown the probe could not reach a verdict, see the reason.
	//
	// The CSI controller works on the export as root, so with a squashed root
	// the PV subdirectories, their deletion and volume cleanup fail or are
	// silently skipped. The condition only warns: it is written by the probe in
	// the CSI controller Pod, not by the module controller, and Ready does not
	// depend on it.
	ConditionTypeRootSquash = "RootSquash"
)

// Reasons of the RootSquash condition.
const (
	// True: the file the probe created is owned by another user, and the
	// server refused to give it back to root.
	ReasonRootSquashed = "RootSquashed"
	// True: the server refused root to create a file in the export root.
	ReasonRootWriteDenied = "RootWriteDenied"
	// False: the server let root hand the file it created to root.
	ReasonRootNotSquashed = "RootNotSquashed"
	// Unknown: the export could not be mounted.
	ReasonMountFailed = "MountFailed"
	// Unknown: the export is read-only, so root cannot write to it whatever
	// the squash settings are.
	ReasonExportReadOnly = "ExportReadOnly"
	// Unknown: the export was mounted, but the probe failed for another reason.
	ReasonProbeFailed = "ProbeFailed"
)

// NFSStorageClassConditionTypes is every condition type an NFSStorageClass
// publishes.
//
// A condition type that is declared but never written is worse than one that
// does not exist: an absent condition is indistinguishable from "not yet
// evaluated", so an operator waits for a verdict that never comes and an alert
// on it never fires. Keeping the set in one list is what lets a test hold the
// writers to it.
var NFSStorageClassConditionTypes = []string{
	ConditionTypeReady,
	ConditionTypeRootSquash,
}

// NFSStorageClassControllerConditionTypes are the condition types the module
// controller writes.
var NFSStorageClassControllerConditionTypes = []string{
	ConditionTypeReady,
}

// NFSStorageClassProbeConditionTypes are the condition types the root-squash
// probe in the CSI controller Pod writes. Together with
// NFSStorageClassControllerConditionTypes they make up
// NFSStorageClassConditionTypes, each type with exactly one writer.
var NFSStorageClassProbeConditionTypes = []string{
	ConditionTypeRootSquash,
}
