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

package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCsiNfs(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.Timeout = suiteTimeout()
	// FailFast stays off, including in CI: a run costs a provisioned cluster, and
	// each scenario is its own Ordered container anyway.
	reporterConfig.Verbose = true
	reporterConfig.ShowNodeEvents = false
	RunSpecs(t, "csi-nfs E2E Suite", suiteConfig, reporterConfig)
}

func suiteTimeout() time.Duration {
	const (
		localDefault = 120 * time.Minute
		ciDefault    = 3*time.Hour + 30*time.Minute
	)

	inCI := os.Getenv("CI") != ""
	timeout := localDefault
	if inCI {
		timeout = ciDefault
	}
	if raw := strings.TrimSpace(os.Getenv("E2E_TEST_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	return timeout
}
