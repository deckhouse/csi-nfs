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
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
)

var volumeSnapshotClassGK = schema.GroupKind{Group: snapshotv1.GroupName, Kind: "VolumeSnapshotClass"}

// VolumeSnapshotClassCRDExists reports whether the VolumeSnapshotClass CRD is registered
// in the cluster. The snapshot CRDs are not shipped by this module and the module no
// longer requires the module that does ship them, so they may legitimately be absent and
// every VolumeSnapshotClass operation has to be skipped in that case.
//
// The manager's RESTMapper is dynamic and reloads discovery on a miss, so the CRD is
// picked up without a controller restart once it is installed in the cluster.
func VolumeSnapshotClassCRDExists(mapper meta.RESTMapper, log logger.Logger) bool {
	_, err := mapper.RESTMapping(volumeSnapshotClassGK, snapshotv1.SchemeGroupVersion.Version)
	switch {
	case err == nil:
		return true
	case meta.IsNoMatchError(err):
		return false
	default:
		log.Error(err, "[VolumeSnapshotClassCRDExists] unable to resolve the VolumeSnapshotClass REST mapping")
		return false
	}
}
