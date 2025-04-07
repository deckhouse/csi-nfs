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

package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/logger"
)

func TestPrioritize(t *testing.T) {
	t.Run("test prioritize", func(t *testing.T) {
		nodeNames := []string{"node1", "node2", "node3"}
		log, err := logger.NewLogger(logger.InfoLevel)
		if err != nil {
			t.Errorf("failed to create logger: %v", err)
		}

		result := scoreNodes(*log, &nodeNames)
		assert.Equal(t, 3, len(result))
		assert.Equal(t, "node1", result[0].Host)
		assert.Equal(t, "node2", result[1].Host)
		assert.Equal(t, "node3", result[2].Host)
		assert.Equal(t, 0, result[0].Score)
		assert.Equal(t, 0, result[1].Score)
		assert.Equal(t, 0, result[2].Score)
	})
}
