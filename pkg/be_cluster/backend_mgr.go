package be_cluster

import (
	"fmt"
	"sync"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/pzhenzhou/elika/pkg/common"
)

var (
	mgrOnce sync.Once
	mgr     *BackendManager
)

type BackendManager struct {
	router        BackendRouter
	balancerRef   Balancer
	config        *common.ProxyConfig
	instancePool  *xsync.MapOf[string, *FixedPool]
	clusterKeyMap *xsync.MapOf[string, *ClusterKey]
}

func GetBackendManager(config *common.ProxyConfig) *BackendManager {
	mgrOnce.Do(func() {
		mgr = &BackendManager{
			config:        config,
			router:        NewBackendRouter(config),
			balancerRef:   NewBalancer(GetBalancerType(&config.Router)),
			instancePool:  xsync.NewMapOf[string, *FixedPool](),
			clusterKeyMap: xsync.NewMapOf[string, *ClusterKey](),
		}
		mgr.PrepareCluster()
	})
	return mgr
}

func (m *BackendManager) backendOffline(instance *ClusterInstance) {
	logger.Info("ProxySrv Backend offline", "instance", instance.GetAddr())
	offlinePool, ok := m.instancePool.LoadAndDelete(instance.GetAddr())
	if ok {
		_ = offlinePool.Close()
	}
}

func (m *BackendManager) backendOnline(instance *ClusterInstance) {
	logger.Info("ProxySrv Backend online", "instance", instance.GetAddr())
	_, ok := m.instancePool.Load(instance.GetAddr())
	if ok {
		logger.Info("ProxySrv Backend already online", "instance", instance.GetAddr())
		return
	}
	tenantKeyStr := instance.EncodeClusterKey()
	tenantCode, _ := common.DecodeBase62(tenantKeyStr)
	logger.Info("ProxySrv BeMgr TenantKeyOnline", "TenantCode", tenantCode)
	m.clusterKeyMap.Store(instance.Owner, &instance.Key)
	poolCfg := NewFixedPoolCfgFromBackend(instance, m.config)
	pool := NewFixedPool(poolCfg)
	pool.WaitPoolReady()
	m.instancePool.Store(instance.GetAddr(), pool)
}

func (m *BackendManager) PrepareCluster() {
	go func(r BackendRouter) {
		r.BackendChangeNotify(func(instance *ClusterInstance) {
			status := instance.Status
			if status == ClusterStatusReady {
				m.backendOnline(instance)
			} else if status == ClusterStatusOffline {
				m.backendOffline(instance)
			} else {
				logger.Info("Backend status not ready", "status", status)
			}
		})
	}(m.router)
}

func (m *BackendManager) GetBackendFixedPool(userName string) (*FixedPool, error) {
	tenantKey := m.GetTenantKey(userName)
	if tenantKey == nil {
		return nil, fmt.Errorf("no tenant key found for auth %+v", userName)
	}
	beInstance, _ := m.router.Selector(m.balancerRef, tenantKey)
	pool, ok := m.instancePool.Load(beInstance.GetAddr())
	if !ok {
		return nil, fmt.Errorf("no backend avaiable for auth %+v", userName)
	}
	return pool, nil
}

func (m *BackendManager) GetTenantKey(userName string) *ClusterKey {
	tk, ok := m.clusterKeyMap.Load(userName)
	if !ok {
		return nil
	}
	return tk
}

func (m *BackendManager) Close() {
	m.instancePool.Range(func(key string, value *FixedPool) bool {
		_ = value.Close()
		return true
	})
}
