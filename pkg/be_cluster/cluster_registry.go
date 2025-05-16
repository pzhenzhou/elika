package be_cluster

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/samber/lo"
)

type ClusterStatus string

const (
	RedisPortName                      = "redis-port"
	ClusterStatusReady   ClusterStatus = "ready"
	ClusterStatusOnline  ClusterStatus = "online"
	ClusterStatusOffline ClusterStatus = "offline"
	ClusterStatusDeleted ClusterStatus = "deleted"
)

type SharedClusterInstance struct {
	mu       sync.RWMutex
	instance []*ClusterInstance
}

func (s *SharedClusterInstance) GetAllClusterForRead() []*ClusterInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instance
}

func NewSharedClusterInstance() *SharedClusterInstance {
	return &SharedClusterInstance{
		instance: make([]*ClusterInstance, 0),
		mu:       sync.RWMutex{},
	}
}

func (s *SharedClusterInstance) UpdateClusterStatus(key ClusterKey, id string, status ClusterStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, idx, ok := lo.FindIndexOf(s.instance, func(item *ClusterInstance) bool {
		if item.Key.Hash() == key.Hash() && item.Id == id {
			return true
		}
		return false
	})
	if ok {
		s.instance[idx].Status = status
	}
}

func (s *SharedClusterInstance) Upsert(info *ClusterInstance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, idx, ok := lo.FindIndexOf(s.instance, func(item *ClusterInstance) bool {
		if item.Key.Hash() == info.Key.Hash() && item.Id == info.Id {
			return true
		}
		return false
	})
	if ok {
		s.instance[idx] = info
	} else {
		s.instance = append(s.instance, info)
	}
}

type ClusterLocation struct {
	Region           string `json:"region"`
	AvailabilityZone string `json:"availability_zone"`
	NodeName         string `json:"node_name"`
}

type Endpoint struct {
	Addr string `json:"addr"`
	Port int    `json:"port"`
	Name string `json:"name"`
}

type ClusterName struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type ClusterKey struct {
	Name     ClusterName     `json:"name"`
	Location ClusterLocation `json:"location,omitempty"`
}

func (k ClusterKey) Hash() uint64 {
	return ClusterKeyHash(k, 0)
}

type ClusterInstance struct {
	Key       ClusterKey        `json:"key"`
	Id        string            `json:"id"`
	Labels    map[string]string `json:"labels,omitempty"`
	Endpoints []Endpoint        `json:"endpoints"`
	Status    ClusterStatus     `json:"status"`
	Owner     string            `json:"owner"`
}

func LocalClusterInstance(addr string, port int) *ClusterInstance {
	return &ClusterInstance{
		Key: ClusterKey{
			Location: ClusterLocation{
				Region:           "local",
				AvailabilityZone: "local",
				NodeName:         "local",
			},
			Name: ClusterName{
				Namespace: "default",
				Name:      "default",
			},
		},
		Id:     "local",
		Labels: nil,
		Endpoints: []Endpoint{
			{
				Addr: addr,
				Port: port,
				Name: RedisPortName,
			},
		},
		Status: ClusterStatusReady,
	}
}

func (c *ClusterInstance) ToBinary() []byte {
	location := c.Key.Location
	clusterName := c.Key.Name
	return []byte(fmt.Sprintf("%s:%s:%s:%s:%s",
		location.Region,
		location.AvailabilityZone,
		location.NodeName,
		clusterName.Namespace, clusterName.Name))
}

func (c *ClusterInstance) EncodeClusterKey() string {
	data := c.ToBinary()
	h := fnv.New64a()
	h.Write(data)
	hash := h.Sum64()
	// Convert to base62 string
	return common.EncodeBase62(hash)
}

func (c *ClusterInstance) HashCode() uint64 {
	fnvHasher := fnv.New64a()
	data := c.ToBinary()
	_, _ = fnvHasher.Write(data)
	return fnvHasher.Sum64()
}

func (c *ClusterInstance) GetAddr() string {
	for _, endpoint := range c.Endpoints {
		if endpoint.Name == RedisPortName {
			return fmt.Sprintf("%s:%d", endpoint.Addr, endpoint.Port)
		}
	}
	return ""
}

func ClusterKeyHash(key ClusterKey, _ uint64) uint64 {
	var builder strings.Builder
	location := key.Location
	builder.WriteString(location.Region)
	builder.WriteString(location.AvailabilityZone)
	builder.WriteString(location.NodeName)
	builder.WriteString(key.Name.Namespace)
	builder.WriteString(key.Name.Name)

	combined := builder.String()

	// Use a simple hash algorithm (FNV-1a)
	var hash uint64 = 14695981039346656037 // FNV offset basis
	for _, c := range []byte(combined) {
		hash ^= uint64(c)
		hash *= 1099511628211 // FNV prime
	}

	// Additional mixing step
	hash = (hash ^ (hash >> 33)) * 0xff51afd7ed558ccd
	hash = (hash ^ (hash >> 33)) * 0xc4ceb9fe1a85ec53
	hash = hash ^ (hash >> 33)

	return hash
}

// ClusterRegistry is a registry for clusters. It is used to manage the clusters and their instances.
type ClusterRegistry interface {
	// AddCluster adds a cluster to the registry.
	AddCluster(key *ClusterKey) error
	// StatusChange updates the status of a cluster. It is used to update the status of a cluster when it is online or offline.
	// Must be after AddCluster.
	StatusChange(instance *ClusterInstance) error
	// Notify is a channel that will be notified when a cluster is added or updated.
	Notify() chan *ClusterInstance
	// GetClusterInstance returns the cluster instance for the given key.
	GetClusterInstance(key ClusterKey) (*SharedClusterInstance, error)
	// AllClusterInstances returns all cluster instances.
	AllClusterInstances() []*ClusterInstance
}

var (
	_                ClusterRegistry = (*DefaultClusterRegistry)(nil)
	registryOnce     sync.Once
	registryInstance *DefaultClusterRegistry
)

type DefaultClusterRegistry struct {
	clusters *xsync.MapOf[ClusterKey, *SharedClusterInstance]
	notify   chan *ClusterInstance
}

func (h *DefaultClusterRegistry) AllClusterInstances() []*ClusterInstance {
	instances := make([]*ClusterInstance, 0)
	h.clusters.Range(func(_ ClusterKey, value *SharedClusterInstance) bool {
		instances = append(instances, value.GetAllClusterForRead()...)
		return true
	})
	return instances
}

func (h *DefaultClusterRegistry) GetClusterInstance(key ClusterKey) (*SharedClusterInstance, error) {
	if v, ok := h.clusters.Load(key); ok {
		return v, nil
	}
	return nil, errors.New("cluster not found")
}

func (h *DefaultClusterRegistry) Notify() chan *ClusterInstance {
	return h.notify
}

func GetClusterRegistry() ClusterRegistry {
	registryOnce.Do(func() {
		registryInstance = newDefaultClusterRegistry()
	})
	return registryInstance
}

func newDefaultClusterRegistry() *DefaultClusterRegistry {
	return &DefaultClusterRegistry{
		clusters: xsync.NewMapOfWithHasher[ClusterKey, *SharedClusterInstance](ClusterKeyHash),
		notify:   make(chan *ClusterInstance, 1024),
	}
}

func (h *DefaultClusterRegistry) AddCluster(key *ClusterKey) error {
	h.clusters.Store(*key, NewSharedClusterInstance())
	return nil
}

func (h *DefaultClusterRegistry) StatusChange(instance *ClusterInstance) error {
	if _, ok := h.clusters.Load(instance.Key); !ok {
		return errors.New("cluster not found")
	}
	h.clusters.Compute(instance.Key, func(oldValue *SharedClusterInstance, loaded bool) (*SharedClusterInstance, bool) {
		oldValue.Upsert(instance)
		return oldValue, false
	})
	h.notify <- instance
	return nil
}
