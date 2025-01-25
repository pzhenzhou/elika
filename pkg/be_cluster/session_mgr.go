package be_cluster

import (
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/respio"
	"net"

	"github.com/puzpuzpuz/xsync/v3"
)

type SessionPair struct {
	session *Session
	backend *BackendConn
}

type SessionManager struct {
	sessions *xsync.MapOf[string, *SessionPair]
	beMgr    *BackendManager
}

func NewSessionManager(config *common.ProxyConfig) *SessionManager {
	return &SessionManager{
		sessions: xsync.NewMapOf[string, *SessionPair](),
		beMgr:    GetBackendManager(config),
	}
}

func (sm *SessionManager) RouteRequest(id string, authInfo *common.AuthInfo) (*SessionPair, error) {
	var err error
	sessionPair, _ := sm.sessions.Compute(id, func(oldValue *SessionPair, loaded bool) (newValue *SessionPair, delete bool) {
		if loaded {
			bindBackendConn := oldValue.backend
			if bindBackendConn != nil {
				txState := bindBackendConn.LoadTxnState()
				if txState == nil || txState.OwnerId == id {
					// No re-routing needed
					return oldValue, false
				}
			}
		}
		// Re-routing needed
		pool, loadErr := sm.beMgr.GetBackendFixedPool(authInfo)
		if loadErr != nil {
			logger.Info("Failed to route request", "SessionId", id, "Error", loadErr)
			err = loadErr
			return oldValue, false
		}
		backendConn, _ := pool.GetConnByKey([]byte(id))
		txState := backendConn.LoadTxnState()
		if txState != nil && txState.OwnerId != id {
			if !common.IsProdRuntime() {
				logger.Info("Current backend cluster has been occupied by another session", "SessionId", id,
					"OtherId", backendConn.LoadTxnState().OwnerId)
			}
			noTxConn, getTxConnErr := pool.GetNoTxConn()
			if getTxConnErr != nil {
				logger.Info("Failed to route request", "SessionId", id, "Error", getTxConnErr)
				err = getTxConnErr
				return oldValue, false
			}
			backendConn = noTxConn
		}
		return &SessionPair{
			session: oldValue.session,
			backend: backendConn,
		}, false
	})
	return sessionPair, err
}

func (sm *SessionManager) Forward(id string, packet *respio.RespPacket, authInfo *common.AuthInfo) error {
	sessionPair, _ := sm.sessions.Load(id)
	backendConn := sessionPair.backend
	needsRoute := false
	if backendConn == nil {
		needsRoute = true
	} else {
		txState := backendConn.LoadTxnState()
		if txState != nil && txState.OwnerId != id {
			needsRoute = true
		}
	}
	if needsRoute {
		newPair, err := sm.RouteRequest(id, authInfo)
		if err != nil {
			return err
		}
		sessionPair = newPair
	}

	// Update transaction state if needed
	if _, state, ok := packet.IsTxCmd(); ok {
		if state == respio.TxCmdStateBegin {
			sessionPair.backend.UpdateTxnState(id, state)
		} else {
			sessionPair.backend.UpdateTxnState("", state)
		}
	}
	reqCtx := RequestContext{
		Session:  sessionPair.session,
		Request:  packet,
		AuthInfo: authInfo,
	}
	sessionPair.backend.Enqueue(&reqCtx)
	return nil
}

func (sm *SessionManager) OpenSession(id string, client net.Conn) {
	session := NewSession(id, client)
	go session.ReplyLoop()
	sm.sessions.Store(id, &SessionPair{session: session})
}

func (sm *SessionManager) LoadSession(id string) *Session {
	if pair, ok := sm.sessions.Load(id); ok {
		return pair.session
	}
	return nil
}

func (sm *SessionManager) CloseSession(id string) {
	if pair, ok := sm.sessions.LoadAndDelete(id); ok {
		pair.session.Close()
	}
}

func (sm *SessionManager) Clear() {
	sm.beMgr.Close()
	sm.sessions.Clear()
}

func (sm *SessionManager) LoadBackendMgr() *BackendManager {
	return sm.beMgr
}
