package be_cluster

import (
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestClusterInstance_EncodeClusterKey(t *testing.T) {
	localInstance := LocalClusterInstance("127.0.0.1", 6379)
	clusterKey := localInstance.EncodeClusterKey()
	clusterKeyHash := localInstance.HashCode()

	t.Logf("ClusterKey: %s, HashCode: %d", clusterKey, clusterKeyHash)

	base62, err := common.DecodeBase62(clusterKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ClusterKey: %s, HashCode: %d %d", clusterKey, clusterKeyHash, base62)

	assert.Equal(t, clusterKeyHash, base62)
}
