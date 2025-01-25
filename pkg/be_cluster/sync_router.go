package be_cluster

var _ BackendRouter = &SyncRouter{}

type SyncRouter struct {
	registry ClusterRegistry
}

func (s *SyncRouter) BackendChangeNotify(notify BackendNotify) {
	notifyChan := s.registry.Notify()
	for {
		select {
		case cluster := <-notifyChan:
			notify(cluster)
		}
	}
}

func (s *SyncRouter) Selector(balancer Balancer, key *ClusterKey) (*ClusterInstance, error) {
	cluster, err := s.registry.GetClusterInstance(*key)
	if err != nil {
		return nil, err
	}
	clusterList := cluster.GetAllClusterForRead()
	if len(clusterList) == 1 {
		return clusterList[0], nil
	}
	nextIdx := int(balancer.Next(key, clusterList))
	return clusterList[nextIdx], nil
}

func (s *SyncRouter) ListBackend(key *ClusterKey) ([]*ClusterInstance, error) {
	cluster, err := s.registry.GetClusterInstance(*key)
	if err != nil {
		return nil, err
	}
	return cluster.GetAllClusterForRead(), nil
}
