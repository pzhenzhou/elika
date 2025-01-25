package be_cluster

import (
	"strings"

	"github.com/pzhenzhou/elika/pkg/common"
)

type BackendNotify func(instance *ClusterInstance)

type BackendRouter interface {
	// BackendChangeNotify is called when the backend changes, This is used to notify the backend manager
	BackendChangeNotify(notify BackendNotify)
	Selector(balancer Balancer, key *ClusterKey) (*ClusterInstance, error)
	ListBackend(key *ClusterKey) ([]*ClusterInstance, error)
}

var _ BackendRouter = &StaticBackendRouter{}

type StaticBackendRouter struct {
	backend *ClusterInstance
}

func (s *StaticBackendRouter) BackendChangeNotify(notify BackendNotify) {
	notify(s.backend)
}

func (s *StaticBackendRouter) Selector(_ Balancer, _ *ClusterKey) (*ClusterInstance, error) {
	return s.backend, nil
}

func (s *StaticBackendRouter) ListBackend(_ *ClusterKey) ([]*ClusterInstance, error) {
	return []*ClusterInstance{s.backend}, nil
}

func NewBackendRouter(conf *common.ProxyConfig) BackendRouter {
	routerType := conf.Router.RouterType
	if routerType == "" || strings.ToLower(routerType) == "static" {
		logger.Info("New static backend router")
		addr, port, err := conf.Router.StatisEndpoint()
		if err != nil {
			panic(err)
		}
		return &StaticBackendRouter{
			backend: LocalClusterInstance(addr, port),
		}
	} else {
		return &SyncRouter{
			registry: GetClusterRegistry(),
		}
	}
}
