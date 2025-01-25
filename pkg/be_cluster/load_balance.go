package be_cluster

import (
	"github.com/pzhenzhou/elika/pkg/common"
	"math/rand"
	"strings"
	"time"
)

type BalancerType int

const (
	BalanceTypeRoundRobin BalancerType = 1 << 4
	BalanceTypeLeastConn  BalancerType = 1 << 5
	BalanceTypeRandom     BalancerType = 1 << 6
)

type Balancer interface {
	Next(tenantKey *ClusterKey, instance []*ClusterInstance) int32
}

var _ Balancer = &RandomBalancer{}

type RandomBalancer struct {
	random *rand.Rand
}

func (r RandomBalancer) Next(_ *ClusterKey, instance []*ClusterInstance) int32 {
	return int32(r.random.Intn(len(instance)))
}

func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func GetBalancerType(config *common.BackendRouterConfig) BalancerType {
	typeStr := strings.ToLower(config.LBType)
	switch typeStr {
	case "random":
		return BalanceTypeRandom
	case "round-robin":
		return BalanceTypeRoundRobin
	case "least-cluster":
		return BalanceTypeLeastConn
	default:
		return BalanceTypeRandom
	}
}

func NewBalancer(balancerType BalancerType) Balancer {
	if balancerType == BalanceTypeRandom {
		return NewRandomBalancer()
	} else {
		panic("Not support this balancer")
	}
}
