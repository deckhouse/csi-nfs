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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/logger"
	"github.com/deckhouse/sds-common-lib/monitoring/readiness"
)

const ReadinessMetricPrefix = "csi_nfs"

func ReadinessSources() []readiness.Source {
	return []readiness.Source{
		readiness.FromList("NFSStorageClass",
			func() client.ObjectList { return &v1alpha1.NFSStorageClassList{} },
			nfsStorageClassConditions),
	}
}

func RegisterReadinessMetric(mgr manager.Manager, log logger.Logger) error {
	return readiness.Register(mgr, ReadinessMetricPrefix, log, ReadinessSources()...)
}
