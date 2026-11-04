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

// Package crdtests checks what kube-apiserver admits and rejects once the
// generated crds/ are installed. It is a module of its own so that envtest, the
// controller-runtime client and sds-common-lib stay out of the requirements of
// the api module, which other modules import.
package crdtests

import (
	"os"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/crdtest"
)

var k8sClient client.Client

func TestMain(m *testing.M) {
	os.Exit(crdtest.Main(m, crdtest.Options{
		CRDPaths:    []string{"../../crds"},
		AddToScheme: []func(*runtime.Scheme) error{v1alpha1.AddToScheme},
	}, func(env *crdtest.Env) { k8sClient = env.Client }))
}

// objectName turns a subtest name into an object name the apiserver accepts.
func objectName(prefix, name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(prefix + "-" + name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if !strings.HasSuffix(b.String(), "-") {
			b.WriteRune('-')
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// expectAdmission checks that err is nil when no messages are given, and
// otherwise that the apiserver rejected the object as invalid with every one of
// messages in the error.
func expectAdmission(t *testing.T, err error, messages ...string) {
	t.Helper()
	if len(messages) == 0 {
		if err != nil {
			t.Fatalf("want the object admitted, got %v", err)
		}
		return
	}
	if !apierrors.IsInvalid(err) {
		t.Fatalf("want an Invalid error with %q, got %v", messages, err)
	}
	for _, message := range messages {
		if !strings.Contains(err.Error(), message) {
			t.Fatalf("want %q in the error, got %v", message, err)
		}
	}
}
