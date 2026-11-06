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
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	"github.com/deckhouse/sds-common-lib/conditions"
)

const (
	// clientCAKey is the key of ConfigMap tlshd-conf with the CA of the client
	// certificate the nodes present on mounts with xprtsec=mtls.
	clientCAKey = "client-ca.crt"
	// clientCARefreshInterval is how often a class with mtls re-reads the CA: the
	// hook renews the client identity months before it expires, and the servers
	// that trust the old CA learn the new one from the status of their class.
	clientCARefreshInterval = 10 * time.Minute
	// clientCAPendingInterval is how often a class with mtls looks for a CA it
	// has not published yet. A new class adds its CA to the truststore, and the
	// chart renders the client CA into the ConfigMap only once a CA is there, so
	// the first pass finds none; an NFS server of the class admits nobody until
	// it learns the CA.
	clientCAPendingInterval = 10 * time.Second
)

// clientCAFor is what status.clientCA of a class says: the CA of the client
// certificate of the nodes, Base64-encoded, for a class with mtls, and nothing
// otherwise or when tlsParameters.mtls of the module sets a certificate without
// its CA. The reader is the API reader, so that the controller needs only "get"
// on the one ConfigMap.
func clientCAFor(ctx context.Context, reader client.Reader, namespace string, nsc *v1alpha1.NFSStorageClass) (string, error) {
	if !commonfeature.TLSEnabled() || !nsc.Spec.Connection.Mtls {
		return "", nil
	}
	configMap := &corev1.ConfigMap{}
	switch err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: tlshdConfigMapName}, configMap); {
	case apierrors.IsNotFound(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting ConfigMap %s/%s: %w", namespace, tlshdConfigMapName, err)
	}
	pem := configMap.Data[clientCAKey]
	if strings.TrimSpace(pem) == "" {
		return "", nil
	}
	return base64.StdEncoding.EncodeToString([]byte(pem)), nil
}

// publishClientCA keeps status.clientCA of a class in step with the client
// identity of the nodes, and says when to look again: soon while a class with
// mtls has no CA to publish, rarely once it has, never without mtls.
func publishClientCA(ctx context.Context, cl client.Client, reader client.Reader, namespace string, nsc *v1alpha1.NFSStorageClass) (time.Duration, error) {
	ca, err := clientCAFor(ctx, reader, namespace, nsc)
	if err != nil {
		return 0, err
	}
	next := time.Duration(0)
	if commonfeature.TLSEnabled() && nsc.Spec.Connection.Mtls {
		next = clientCARefreshInterval
		if ca == "" {
			next = clientCAPendingInterval
		}
	}
	if nsc.Status != nil && nsc.Status.ClientCA == ca {
		return next, nil
	}
	return next, conditions.UpdateStatus(ctx, cl, nsc, func(sc *v1alpha1.NFSStorageClass) {
		if sc.Status == nil {
			sc.Status = &v1alpha1.NFSStorageClassStatus{}
		}
		sc.Status.ClientCA = ca
	})
}
