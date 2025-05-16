package be_cluster

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/pzhenzhou/elika/pkg/common"
)

var (
	// timers is a innerPool of time.Timer objects that can cluster reused.
	// Pools of ephemeral objects
	ephemeralTimers = sync.Pool{
		New: func() interface{} {
			t := time.NewTimer(time.Hour)
			t.Stop()
			return t
		},
	}
)

var (
	// ErrClosed performs any operation on the closed client will return this error.
	ErrClosed = errors.New("elika proxy: client is closed")

	// ErrPoolExhausted is returned from a innerPool connection method
	// when the maximum number of database connections in the innerPool has been reached.
	ErrPoolExhausted = errors.New("elika proxy: connection innerPool exhausted")

	// ErrPoolTimeout timed out waiting to get a connection from the connection innerPool.
	ErrPoolTimeout        = errors.New("elika proxy: connection innerPool timeout")
	defaultTryDialBackoff = backoff.WithMaxElapsedTime(30 * time.Minute)
)

type PoolConfig struct {
	Addr       string
	Credential atomic.Value
	Dialer     func(context.Context) (*BackendConn, error) `json:"-"`
	// PoolSize is the size of the innerPool
	PoolSize int
	// MaxIdleSize is the max idle size of the innerPool
	MaxIdleSize int
	// MinIdleSize is the min idle size of the innerPool
	MinIdleSize int
	// MaxActiveSize is the max active size of the innerPool
	MaxActiveSize int
	// MinActiveSize is the min active size of the innerPool
	MinActiveSize   int
	ConnMaxLifetime time.Duration
	PoolWaitTimeout time.Duration
}

type BackendPoolStatus struct {
	// ImmediateGets Got connection without waiting
	ImmediateGets uint32
	// DelayedGets Had to wait for connection
	DelayedGets uint32
	// Timeouts Wait for connection timed out
	Timeouts uint32
	// TotalConns is the total number of connections in the innerPool
	Conns uint32
	// IdleConns is the number of idle connections in the innerPool
	IdleConns uint32
	// StaleConns  Remove/Expired connections
	StaleConns uint32
}

func NewFixedPoolCfgFromBackend(instance *ClusterInstance, config *common.ProxyConfig) *PoolConfig {
	cfg := &PoolConfig{
		Addr:            instance.GetAddr(),
		PoolSize:        config.BeConnPool.MaxSize,
		MaxIdleSize:     config.BeConnPool.MaxSize,
		MinIdleSize:     config.BeConnPool.MaxSize,
		MaxActiveSize:   10,
		PoolWaitTimeout: 1 * time.Second,
		ConnMaxLifetime: 0,
	}
	cfg.Dialer = func(ctx context.Context) (*BackendConn, error) {
		return NewBackendConn(3*time.Second, cfg.Addr, 10240)
	}
	return cfg
}

func NewDefaultPoolCfgFromBackend(instance *ClusterInstance, config *common.ProxyConfig) *PoolConfig {
	cfg := &PoolConfig{
		Addr:            instance.GetAddr(),
		PoolSize:        config.BeConnPool.MaxSize,
		MaxIdleSize:     config.BeConnPool.MaxIdle,
		MinIdleSize:     1,
		MaxActiveSize:   10,
		PoolWaitTimeout: 1 * time.Second,
		ConnMaxLifetime: 0,
	}
	cfg.Dialer = func(ctx context.Context) (*BackendConn, error) {
		return NewBackendConn(3*time.Second, cfg.Addr, 10240)
	}
	return cfg
}

type BackendPool struct {
	cfg       *PoolConfig
	queue     chan struct{}
	conns     []*BackendConn
	idleConns []*BackendConn
	// errNums is the number of errors that occurred when creating a connection
	errNums     uint32
	mu          sync.Mutex
	createConn  int
	idleConnLen int
	closed      uint32
	status      *BackendPoolStatus
	addr        string
	lastDialErr atomic.Value
}

func NewBackendConnPool(cfg *PoolConfig) *BackendPool {
	pool := &BackendPool{
		cfg:       cfg,
		mu:        sync.Mutex{},
		queue:     make(chan struct{}, cfg.PoolSize),
		conns:     make([]*BackendConn, 0, cfg.PoolSize),
		idleConns: make([]*BackendConn, 0, cfg.PoolSize),
		status:    &BackendPoolStatus{},
	}
	pool.mu.Lock()
	pool.checkMinIdleConns()
	pool.mu.Unlock()
	return pool
}

func (p *BackendPool) checkMinIdleConns() {
	if p.cfg.MinIdleSize == 0 {
		return
	}
	for p.createConn < p.cfg.PoolSize && p.idleConnLen < p.cfg.MinIdleSize {
		select {
		// get a slot from the queue, if the queue is full, it will block
		// An operation equivalent to a semaphore. sem.TryAcquire(1).
		case p.queue <- struct{}{}:
			p.createConn++
			p.idleConnLen++

			go func() {
				err := p.addIdleConn()
				if err != nil && !errors.Is(err, ErrClosed) {
					logger.Error(err, "add idle cluster failed", "addr", p.cfg.Addr)
					p.mu.Lock()
					p.createConn--
					p.idleConnLen--
					p.mu.Unlock()
				}
				p.freeSlot()
			}()
		default:
			return
		}
	}
}

func (p *BackendPool) addIdleConn() error {
	if p.IsClosed() {
		return ErrClosed
	}
	conn, err := p.dialConn(context.TODO())
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns = append(p.conns, conn)
	p.idleConns = append(p.idleConns, conn)
	return nil
}

func (p *BackendPool) IsClosed() bool {
	return atomic.LoadUint32(&p.closed) == 1
}

func (p *BackendPool) freeSlot() {
	<-p.queue
}

func (p *BackendPool) testConn() {
	backend, err := backoff.Retry[*BackendConn](context.Background(), func() (*BackendConn, error) {
		if p.IsClosed() {
			return nil, nil
		}
		testConn, dialErr := p.cfg.Dialer(context.Background())
		if dialErr != nil {
			p.lastDialErr.Store(dialErr)
			return nil, dialErr
		}
		atomic.StoreUint32(&p.errNums, 0)
		return testConn, nil
	}, defaultTryDialBackoff)

	if err == nil && backend != nil {
		_ = backend.Close()
	}
}

// getIdleConn returns a idle connection from the innerPool
// this func must cluster called in a lock
func (p *BackendPool) getIdleConn() (*BackendConn, error) {
	if p.IsClosed() {
		return nil, ErrClosed
	}
	idleLen := len(p.idleConns)
	if idleLen == 0 {
		return nil, nil
	}
	var conn *BackendConn
	lastIdx := idleLen - 1
	conn = p.idleConns[lastIdx]
	p.idleConns = p.idleConns[:lastIdx]
	p.idleConnLen--
	p.checkMinIdleConns()
	return conn, nil
}

func (p *BackendPool) dialConn(ctx context.Context) (*BackendConn, error) {
	if p.IsClosed() {
		return nil, ErrClosed
	}
	if atomic.LoadUint32(&p.errNums) >= uint32(p.cfg.PoolSize) {
		return nil, p.lastDialError()
	}
	backendConn, err := p.cfg.Dialer(ctx)
	if err != nil {
		p.lastDialErr.Store(err)
		go p.testConn()
		return nil, err
	}
	return backendConn, nil
}

func (p *BackendPool) lastDialError() error {
	if v := p.lastDialErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

func (p *BackendPool) Put(backend *BackendConn) {
	if p.IsClosed() {
		logger.Info("WARN: put cluster to closed innerPool", "addr", p.cfg.Addr)
		return
	}
	if backend.reader.Buffered() > 0 {
		logger.Info("WARN: put cluster with buffered data", "addr", backend.RemoteAddr(),
			"DataBuffer", backend.reader.Buffered())
		_ = p.removeConnAndClose(backend)
		p.freeSlot()
		return
	}
	var closeConn bool
	p.mu.Lock()
	if p.cfg.MaxIdleSize == 0 || p.idleConnLen < p.cfg.MaxIdleSize {
		p.idleConns = append(p.idleConns, backend)
		p.idleConnLen++
	} else {
		p.tryRemoveConn(backend)
		closeConn = true
	}
	p.mu.Unlock()
	p.freeSlot()
	if closeConn {
		_ = backend.Close()
	}
}

func (p *BackendPool) Get(ctx context.Context) (*BackendConn, error) {
	if p.IsClosed() {
		return nil, ErrClosed
	}

	if err := p.getSlot(ctx); err != nil {
		return nil, err
	}
	for {
		p.mu.Lock()
		conn, err := p.getIdleConn()
		p.mu.Unlock()
		if err != nil {
			p.freeSlot()
			return nil, err
		}
		// not found idle conn. create a new conn
		if conn == nil {
			break
		}
		// conn is not nil, check the conn health, if the conn is not healthy, close it and create a new conn
		if !p.health(conn) {
			_ = p.removeConnAndClose(conn)
			continue
		}
		atomic.AddUint32(&p.status.ImmediateGets, 1)
		return conn, nil
	}
	atomic.AddUint32(&p.status.DelayedGets, 1)
	newConn, err := p.makeConn(ctx)
	if err != nil {
		return nil, err
	}
	return newConn, nil
}

func (p *BackendPool) makeConn(ctx context.Context) (*BackendConn, error) {
	if p.IsClosed() {
		return nil, ErrClosed
	}
	p.mu.Lock()
	if p.cfg.MaxActiveSize > 0 && p.createConn >= p.cfg.MaxActiveSize {
		p.mu.Unlock()
		return nil, ErrPoolExhausted
	}
	p.mu.Unlock()
	conn, err := p.dialConn(ctx)
	if err != nil {
		logger.Error(err, "dial cluster failed", "addr", p.cfg.Addr)
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.MaxActiveSize > 0 && p.createConn >= p.cfg.MaxActiveSize {
		_ = conn.Close()
		return nil, ErrPoolExhausted
	}
	p.conns = append(p.conns, conn)
	p.createConn++

	return conn, nil
}

func (p *BackendPool) health(backendConn *BackendConn) bool {
	now := time.Now()
	if p.cfg.ConnMaxLifetime > 0 && now.Sub(backendConn.created) > p.cfg.ConnMaxLifetime {
		return false
	}
	if p.cfg.ConnMaxLifetime > 0 && now.Sub(backendConn.UsedAt()) > p.cfg.ConnMaxLifetime {
		return false
	}
	if checkConn(backendConn.conn) != nil {
		return false
	}
	backendConn.SetUsedAt(now)
	return true
}

func (p *BackendPool) getSlot(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	select {
	case p.queue <- struct{}{}:
		return nil
	default:
	}

	timer := ephemeralTimers.Get().(*time.Timer)
	timer.Reset(p.cfg.PoolWaitTimeout)

	select {
	case p.queue <- struct{}{}:
		if !timer.Stop() {
			<-timer.C
		}
		ephemeralTimers.Put(timer)
		return nil
	case <-timer.C:
		atomic.AddUint32(&p.status.Timeouts, 1)
		return ErrPoolTimeout
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		ephemeralTimers.Put(timer)
		return ctx.Err()
	}
}

func (p *BackendPool) removeConnAndClose(backendConn *BackendConn) error {
	p.tryRemoveConn(backendConn)
	return backendConn.Close()
}

func (p *BackendPool) tryRemoveConn(backendConn *BackendConn) {
	for i, conn := range p.conns {
		if conn.conn == backendConn.conn {
			p.conns = append(p.conns[:i], p.conns[i+1:]...)
			p.createConn--
			p.checkMinIdleConns()
			break
		}
	}
	atomic.AddUint32(&p.status.StaleConns, 1)
}

func (p *BackendPool) resetPool() {
	p.conns = nil
	p.createConn = 0
	p.idleConns = nil
	p.idleConnLen = 0
}

func (p *BackendPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

func (p *BackendPool) PoolStatus() *BackendPoolStatus {
	return &BackendPoolStatus{
		ImmediateGets: atomic.LoadUint32(&p.status.ImmediateGets),
		DelayedGets:   atomic.LoadUint32(&p.status.DelayedGets),
		Timeouts:      atomic.LoadUint32(&p.status.Timeouts),
		Conns:         atomic.LoadUint32(&p.status.Conns),
		IdleConns:     atomic.LoadUint32(&p.status.IdleConns),
		StaleConns:    atomic.LoadUint32(&p.status.StaleConns),
	}
}

func (p *BackendPool) IsAuthInfoSet() bool {
	return p.cfg.Credential.Load() != nil
}

// SetAuthInfo sets the auth info for the innerPool
// This method will cluster called only after the authentication is successful.
func (p *BackendPool) SetAuthInfo(info *common.AuthInfo) {
	p.cfg.Credential.Swap(info)
}

func (p *BackendPool) LoadAuthInfo() *common.AuthInfo {
	auth, ok := p.cfg.Credential.Load().(*common.AuthInfo)
	if !ok {
		return nil
	}
	return auth
}

func (p *BackendPool) Close() error {
	if !atomic.CompareAndSwapUint32(&p.closed, 0, 1) {
		return ErrClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var returnErr error
	for _, conn := range p.conns {
		if err := conn.Close(); err != nil && returnErr == nil {
			if !errors.Is(err, net.ErrClosed) {
				logger.Error(err, "close cluster failed")
				returnErr = err
			}
		}
	}
	// reset cluster
	p.resetPool()
	return returnErr
}
