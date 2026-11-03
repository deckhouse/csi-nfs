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

// Package tlscabundle gathers the CAs that verify the NFS servers into the one
// truststore tlshd reads on every node: tlsParameters.ca of the module and
// spec.tlsParameters.ca of every NFSStorageClass.
package tlscabundle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sort"
	"time"

	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"

	"github.com/deckhouse/csi-nfs/hooks/go/consts"
)

const (
	nfsStorageClassCASnapshot = "nfs-storage-class-cas"
	// The hook runs after 080, which watches the same objects.
	beforeHelmOrder = 90
)

var (
	moduleCAPath = fmt.Sprintf("%s.tlsParameters.ca", consts.ModuleName)
	// BundlePath is where the templates read the truststore from; empty when no
	// CA is configured anywhere, which is what keeps tlshd off the nodes.
	BundlePath = fmt.Sprintf("%s.internal.tlsCABundle", consts.ModuleName)
)

var _ = registry.RegisterFunc(
	&pkg.HookConfig{
		OnBeforeHelm: &pkg.OrderedConfig{Order: beforeHelmOrder},
		Kubernetes: []pkg.KubernetesConfig{
			{
				Name:                         nfsStorageClassCASnapshot,
				APIVersion:                   "storage.deckhouse.io/v1alpha1",
				Kind:                         "NFSStorageClass",
				ExecuteHookOnEvents:          ptr(true),
				ExecuteHookOnSynchronization: ptr(true),
				JqFilter:                     `{"name": .metadata.name, "ca": (.spec.tlsParameters.ca // "")}`,
				AllowFailure:                 ptr(true),
			},
		},
		Settings: &pkg.HookConfigSettings{
			ExecutionMinInterval: 3 * time.Second,
			ExecutionBurst:       1,
		},
		Queue: fmt.Sprintf("modules/%s", consts.ModuleName),
	},
	handle,
)

// ClassCA is the CA of one NFSStorageClass, as the snapshot carries it.
type ClassCA struct {
	Name string `json:"name"`
	CA   string `json:"ca"`
}

func handle(_ context.Context, input *pkg.HookInput) error {
	var classes []ClassCA
	for _, s := range input.Snapshots.Get(nfsStorageClassCASnapshot) {
		var c ClassCA
		if err := s.UnmarshalTo(&c); err != nil {
			return fmt.Errorf("reading the snapshot of an NFSStorageClass: %w", err)
		}
		classes = append(classes, c)
	}
	bundle, skipped := Bundle(input.Values.Get(moduleCAPath).String(), classes)
	for _, name := range skipped {
		input.Logger.Warn("the tlsParameters.ca of the NFSStorageClass holds no PEM certificate and is left out of the truststore", "nfsStorageClass", name)
	}
	input.Values.Set(BundlePath, bundle)
	return nil
}

// Bundle returns the Base64 encoding of every distinct certificate in the CA
// of the module and the CAs of the classes, the module's first and then the
// classes by name, so that the same set always renders the same ConfigMap.
// It also names the classes whose CA holds no certificate; the webhook keeps
// such a class out, so the list is a guard against objects created before it.
func Bundle(moduleCA string, classes []ClassCA) (string, []string) {
	sorted := append([]ClassCA(nil), classes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var out bytes.Buffer
	seen := map[string]bool{}
	add := func(encoded string) bool {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return false
		}
		found := false
		for rest := data; ; {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			found = true
			if seen[string(block.Bytes)] {
				continue
			}
			seen[string(block.Bytes)] = true
			_ = pem.Encode(&out, block)
		}
		return found
	}

	// The CA of the module goes in as before even when it holds no PEM
	// certificate: tlshd has always read it as given, and a cluster that relied
	// on that keeps the truststore it had.
	if moduleCA != "" && !add(moduleCA) {
		if data, err := base64.StdEncoding.DecodeString(moduleCA); err == nil {
			out.Write(data)
		}
	}
	var skipped []string
	for _, c := range sorted {
		if c.CA != "" && !add(c.CA) {
			skipped = append(skipped, c.Name)
		}
	}
	if out.Len() == 0 {
		return "", skipped
	}
	return base64.StdEncoding.EncodeToString(out.Bytes()), skipped
}

func ptr[T any](a T) *T {
	return &a
}
