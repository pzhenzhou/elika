## Elika Proxy

Cloud-Native Redis Proxy for Serverless and Multi-Tenant Environments

## Overview

Elika Proxy is a sophisticated middleware solution that not only forwards Redis commands but serves as a comprehensive
management layer for Redis clusters in cloud-native environments. Built with multi-tenancy in mind, it provides a
unified entry point for multiple Redis clusters while offering advanced capabilities for cluster management, scaling,
and monitoring.

### Features

1. Lightweight & High Performance
2. Multi-tenants support.
3. RESP2/RESP3 protocol support.
4. Backend Connection Pooling and multiplexing.

## Extensibility

Implement your own ClusterRegistry interface to:

- Integrate with your control plane
- Implement custom service discovery
- Add cluster health monitoring
- Handle scaling events

```golang
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
```

### Local Development

```bash
make build 
make run-local ARGS="--router.type=static --router.static-be=127.0.0.1:6379"
```

```text

        ______     __         __     __  __     ______        ______   ______     ______     __  __     __  __
        /\  ___\   /\ \       /\ \   /\ \/ /    /\  __ \      /\  == \ /\  == \   /\  __ \   /\_\_\_\   /\ \_\ \
        \ \  __\   \ \ \____  \ \ \  \ \  _"-.  \ \  __ \     \ \  _-/ \ \  __<   \ \ \/\ \  \/_/\_\/_  \ \____ \
         \ \_____\  \ \_____\  \ \_\  \ \_\ \_\  \ \_\ \_\     \ \_\    \ \_\ \_\  \ \_____\   /\_\/\_\  \/\_____\
          \/_____/   \/_____/   \/_/   \/_/\/_/   \/_/\/_/      \/_/     \/_/ /_/   \/_____/   \/_/\/_/   \/_____/


2025-01-25T22:17:03.399+0800    INFO    backend conn/router.go:39       New static backend router
2025-01-25T22:17:03.399+0800    INFO    backend conn/backend_mgr.go:47  ProxySrv Backend online {"instance": "127.0.0.1:6379"}
2025-01-25T22:17:03.399+0800    INFO    backend conn/backend_mgr.go:55  ProxySrv BeMgr TenantKeyOnline  {"TenantCode": 3422909936434757924}
2025-01-25T22:17:03.399+0800    INFO    main    proxy/main.go:56        Starting cmux proxy...  {"ServiceAddr": "[::]:7080"}
2025-01-25T22:17:03.400+0800    INFO    proxy-srv       proxy/elika_proxy.go:50 Starting ElikaProxy     {"address": "tcp://:6378"}

```

```
redis-cli -p 6378
127.0.0.1:6378> auth yigF5wkxq44.admin admin
OK
127.0.0.1:6378> ping
PONG
127.0.0.1:6378> set key1 value1
OK
```