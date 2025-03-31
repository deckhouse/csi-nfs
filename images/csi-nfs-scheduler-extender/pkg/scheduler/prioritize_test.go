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
