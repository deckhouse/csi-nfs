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

package handlers

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/slok/kubewebhook/v2/pkg/model"
	authenticationv1 "k8s.io/api/authentication/v1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/deckhouse/sds-common-lib/logger"
)

func TestValidatorsLogThroughSetLogger(t *testing.T) {
	const message = "is allowed to manage storage classes"

	tests := []struct {
		level   logger.Verbosity
		wantLog bool
	}{
		{level: logger.InfoLevel, wantLog: true},
		{level: logger.ErrorLevel, wantLog: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			var out bytes.Buffer
			l, err := logger.NewLoggerToWriter(&out, tt.level)
			if err != nil {
				t.Fatal(err)
			}
			SetLogger(l)
			t.Cleanup(func() { SetLogger(logger.NewNop()) })

			ar := &model.AdmissionReview{UserInfo: authenticationv1.UserInfo{Username: allowedUserName}}
			sc := &storagev1.StorageClass{Provisioner: NFSStorageClassProvisioner}
			if _, err := SCValidate(context.Background(), ar, sc); err != nil {
				t.Fatal(err)
			}

			if got := strings.Contains(out.String(), message); got != tt.wantLog {
				t.Fatalf("message logged = %t, want %t; output: %q", got, tt.wantLog, out.String())
			}
		})
	}
}
