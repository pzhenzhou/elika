package be_cluster

import (
	"errors"
	"github.com/pzhenzhou/elika/pkg/respio"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/buraksezer/consistent"
	"github.com/cespare/xxhash/v2"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/pzhenzhou/elika/pkg/common"
)

type Member struct {
	key string
}

func (m Member) String() string {
	return m.key
}

type memberHash struct{}

func (h memberHash) Sum64(key []byte) uint64 {
	return xxhash.Sum64(key)
}

var (
	consistentCfg = consistent.Config{
		PartitionCount: 256,
		Load:           1.25,
		Hasher:         memberHash{},
	}
)

type FixedPool struct {
	fixedCfg  *PoolConfig
	innerPool *BackendPool
	ready     uint32
	onLines   *xsync.MapOf[string, *BackendConn]
	cHasher   *consistent.Consistent
}

func NewFixedPool(cfg *PoolConfig) *FixedPool {
	return &FixedPool{
		fixedCfg:  cfg,
		innerPool: NewBackendConnPool(cfg),
		onLines:   xsync.NewMapOf[string, *BackendConn](),
		cHasher:   consistent.New(nil, consistentCfg),
	}
}

func (f *FixedPool) IsReady() bool {
	return atomic.LoadUint32(&f.ready) == 1
}

func (f *FixedPool) SetAuthInfo(auth *common.AuthInfo) {
	f.innerPool.SetAuthInfo(auth)
}

func (f *FixedPool) WaitPoolReady() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	size := f.fixedCfg.PoolSize
	for {
		select {
		case <-ticker.C:
			if f.innerPool.Size() == size {
				for _, conn := range f.innerPool.conns {
					f.onLines.Store(conn.Id, conn)
					f.cHasher.Add(Member{
						key: conn.Id,
					})
				}
				atomic.StoreUint32(&f.ready, 1)
				return
			}
		}
	}
}

func (f *FixedPool) GetNoTxConn() (*BackendConn, error) {
	var candidates []*BackendConn
	f.onLines.Range(func(key string, conn *BackendConn) bool {
		if txState := conn.LoadTxnState(); txState == nil || txState.State == "" || txState.State == respio.TxCmdStateEnd {
			candidates = append(candidates, conn)
		}
		return true
	})
	candidatesLen := len(candidates)
	if candidatesLen == 0 {
		return nil, errors.New("no connection found")
	}
	if candidatesLen == 1 {
		return candidates[0], nil
	}
	for {
		randIdx := rand.IntN(candidatesLen)
		conn := candidates[randIdx]
		if conn.LoadTxnState() != nil && conn.LoadTxnState().State == respio.TxCmdStateBegin {
			continue
		}
		return conn, nil
	}
}

func (f *FixedPool) GetConnByKey(key []byte) (*BackendConn, error) {
	member := f.cHasher.LocateKey(key)
	if conn, ok := f.onLines.Load(member.String()); ok {
		return conn, nil
	}
	return nil, errors.New("no connection found")
}

func (f *FixedPool) Close() error {
	return f.innerPool.Close()
}
