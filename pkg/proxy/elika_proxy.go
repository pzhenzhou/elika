package proxy

import (
	"context"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"github.com/pzhenzhou/elika/pkg/be_cluster"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/metrics"
	"github.com/pzhenzhou/elika/pkg/respio"
	"io"
	"strings"
)

const (
	Banner = `

	______     __         __     __  __     ______        ______   ______     ______     __  __     __  __    
	/\  ___\   /\ \       /\ \   /\ \/ /    /\  __ \      /\  == \ /\  == \   /\  __ \   /\_\_\_\   /\ \_\ \   
	\ \  __\   \ \ \____  \ \ \  \ \  _"-.  \ \  __ \     \ \  _-/ \ \  __<   \ \ \/\ \  \/_/\_\/_  \ \____ \  
	 \ \_____\  \ \_____\  \ \_\  \ \_\ \_\  \ \_\ \_\     \ \_\    \ \_\ \_\  \ \_____\   /\_\/\_\  \/\_____\ 
	  \/_____/   \/_____/   \/_/   \/_/\/_/   \/_/\/_/      \/_/     \/_/ /_/   \/_____/   \/_/\/_/   \/_____/ 
																											   
                                                                                                                             
`
)

var (
	logger = common.InitLogger().WithName("proxy-srv")
)

type ElikaProxyServer struct {
	gnet.BuiltinEventEngine
	eng               *gnet.Engine
	config            *common.ProxyConfig
	sessionMgr        *be_cluster.SessionManager
	metricsMiddleware *metrics.ProxyMetricsMiddleWare
}

func NewElikaProxy(config *common.ProxyConfig) *ElikaProxyServer {
	proxySrv := &ElikaProxyServer{
		config:     config,
		sessionMgr: be_cluster.NewSessionManager(config),
	}
	return proxySrv
}

func (p *ElikaProxyServer) SetMetricsMiddleware(middleware *metrics.ProxyMetricsMiddleWare) {
	p.metricsMiddleware = middleware
}

func (p *ElikaProxyServer) Start() error {
	opts := p.config.GNetOptions()
	opts = append(opts, gnet.WithReuseAddr(true), gnet.WithReusePort(true))
	proxyAddr := fmt.Sprintf("tcp://:%d", p.config.ProxyPort)
	logger.Info("Starting ElikaProxy", "address", proxyAddr)
	var err error
	if len(opts) > 0 {
		err = gnet.Run(p, proxyAddr, opts...)
	} else {
		err = gnet.Run(p, proxyAddr)
	}
	return err
}

func (p *ElikaProxyServer) OnBoot(eng gnet.Engine) gnet.Action {
	p.eng = &eng
	return gnet.None
}

func (p *ElikaProxyServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	connId := c.RemoteAddr().String()
	p.sessionMgr.OpenSession(connId, c)
	// logger.Info("ElikaProxy opened connection", "connId", connId)
	return nil, gnet.None
}

func (p *ElikaProxyServer) doForward(id string, session *be_cluster.Session, authInfo *common.AuthInfo, packet *respio.RespPacket) error {
	if err := p.sessionMgr.Forward(id, packet, authInfo); err != nil {
		return session.WriteAndFlush(&respio.RespPacket{
			Type: respio.RespError,
			Data: []byte(err.Error()),
		})
	}
	return nil
}

func (p *ElikaProxyServer) forward(id string, session *be_cluster.Session, authInfo *common.AuthInfo, packet *respio.RespPacket) error {
	if p.metricsMiddleware != nil {
		return p.metricsMiddleware.WrapForwarding(packet, func() error {
			return p.doForward(id, session, authInfo, packet)
		})
	}
	return p.doForward(id, session, authInfo, packet)
}

func (p *ElikaProxyServer) doDispatch(client *be_cluster.Session, packet *respio.RespPacket) error {
	var authInfo *common.AuthInfo
	if client.IsAuthenticated() {
		authInfo = client.GetAuthInfo()
		return p.forward(client.Id, client, authInfo, packet)
	}
	if !packet.IsAuthCmd() {
		return client.WriteAndFlush(respio.ErrNoAuth)
	}
	authInfo = packet.ToAuthInfo()
	if authInfo.LoadTenantCode() == 0 {
		return client.WriteAndFlush(respio.ErrAuthFailed)
	}
	var authPacket *respio.RespPacket
	if strings.EqualFold(p.config.Router.RouterType, "sync") {
		authPacket = respio.NewAuthPacket(nil, authInfo.Password)
	} else {
		authPacket = respio.NewAuthPacket(authInfo.Username, authInfo.Password)
	}
	return p.forward(client.Id, client, authInfo, authPacket)
}

func (p *ElikaProxyServer) dispatch(client *be_cluster.Session, packet *respio.RespPacket) error {
	if p.metricsMiddleware != nil {
		return p.metricsMiddleware.WrapDispatch(packet, func() error {
			return p.doDispatch(client, packet)
		})
	}
	return p.doDispatch(client, packet)
}

// OnTraffic We model connection lifecycle in two phases: AUTH and Command
// Client          Proxy          Backend
//
//	|              |              |
//	|--Connect---->|              |
//	|              |              |
//	|--AUTH------->|              |
//	|              |--AUTH------->|
//	|              |<--OK---------|
//	|<--OK---------|              |
//	|              |              |
//	|--Command---->|              |
//	|              |--Command---->|
//	|              |<--Response---|
//	|<--Response---|              |
func (p *ElikaProxyServer) OnTraffic(c gnet.Conn) gnet.Action {
	connId := c.RemoteAddr().String()
	client := p.sessionMgr.LoadSession(connId)
	if p.metricsMiddleware != nil {
		return p.metricsMiddleware.WrapTraffic(func() gnet.Action {
			return p.onEvent(client)
		})
	}
	return p.onEvent(client)
}

func (p *ElikaProxyServer) onEvent(client *be_cluster.Session) gnet.Action {
	for {
		packet, err := client.Read()
		if err != nil {
			if err == io.EOF {
				return gnet.None
			}
			return gnet.Close
		}
		processErr := p.dispatch(client, packet)
		if processErr != nil {
			logger.Error(processErr, "Error processing client request", "clientId", client.Id)
			return gnet.None
		}
		if client.ReadBuffered() == 0 {
			break
		}
	}
	return gnet.None
}

func (p *ElikaProxyServer) OnClose(c gnet.Conn, err error) gnet.Action {
	connId := c.RemoteAddr().String()
	logger.Info("ElikaProxy closed connection", "connId", connId, "err", err)
	p.sessionMgr.CloseSession(connId)
	return gnet.Close
}

func (p *ElikaProxyServer) OnShutdown(eng gnet.Engine) {
	if eng.Validate() != nil {
		return
	}
	logger.Info("ElikaProxy is shutting down. cleaning up resources")
	p.sessionMgr.Clear()
}

func (p *ElikaProxyServer) Shutdown(ctx context.Context) {
	if err := p.eng.Stop(ctx); err != nil {
		logger.Error(err, "Failed to stop proxy proxy")
	} else {
		logger.Info("Proxy proxy stopped")
	}
}
