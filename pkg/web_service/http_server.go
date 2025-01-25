package web_service

import (
	"context"
	"errors"
	"github.com/gin-contrib/pprof"
	"github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/pzhenzhou/elika/pkg/be_cluster"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/samber/lo"
	"github.com/soheilhy/cmux"
	"net/http"
	"strings"
	"time"
)

type HttpMethod string

const (
	GET    HttpMethod = "GET"
	POST   HttpMethod = "POST"
	PUT    HttpMethod = "PUT"
	DELETE HttpMethod = "DELETE"
)

const (
	StateKeyBackendManager = "BackendManager"
	ClusterRegistryKey     = "ClusterRegistry"
)

type ApiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

var (
	logger = common.InitLogger().WithName("web")
)

type WebHandler interface {
	Path() string
	Method() HttpMethod
	Handler(ctx *gin.Context)
}

type WebServer struct {
	enablePprof bool
	port        int
	r           *gin.Engine
	server      *http.Server
	handlers    []WebHandler
}

func NewWebServer(config *common.ProxyConfig) *WebServer {
	allHandler := []WebHandler{
		&HealthCheckHandler{},
	}
	if config.Router.RouterType == "sync" {
		allHandler = append(allHandler, &AddTenantHandler{},
			&ListAllTenantsHandler{})
	}
	return NewWebServerWithHandlers(config, allHandler)
}

func NewWebServerWithHandlers(config *common.ProxyConfig, handlers []WebHandler) *WebServer {
	srv := initWebServer(config)
	for _, handler := range handlers {
		srv.registerHandler(handler)
	}
	return srv
}

func GlobalBackendManager(config *common.ProxyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(StateKeyBackendManager, be_cluster.GetBackendManager(config))
		c.Next()
	}
}

func GlobalClusterRegistry() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ClusterRegistryKey, be_cluster.GetClusterRegistry())
		c.Next()
	}
}

func initWebServer(config *common.ProxyConfig) *WebServer {
	r := gin.New()
	enablePprof := config.WebServer.EnablePprof
	zapLogger := common.RawZapLogger()
	if config.Router.RouterType == "sync" {
		r.Use(GlobalBackendManager(config))
		r.Use(GlobalClusterRegistry())
	}
	r.Use(ginzap.RecoveryWithZap(zapLogger, true))
	r.Use(ginzap.GinzapWithConfig(zapLogger, &ginzap.Config{
		UTC:        true,
		TimeFormat: time.RFC3339,
		Skipper: func(c *gin.Context) bool {
			if strings.HasPrefix(c.Request.URL.Path, "debug") {
				return true
			}
			return c.Request.URL.Path == "/healthz" && c.Request.Method == "GET"
		},
	}))
	if enablePprof {
		pprof.Register(r)
	}
	if common.IsProdRuntime() {
		gin.SetMode(gin.ReleaseMode)
	}
	return &WebServer{
		r:        r,
		handlers: make([]WebHandler, 0),
	}
}

func (s *WebServer) Start(m cmux.CMux) error {
	httpL := m.Match(cmux.HTTP1Fast())
	httpServer := &http.Server{
		Handler: s.r,
	}
	s.server = httpServer
	if err := httpServer.Serve(httpL); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		logger.Error(err, "Failed to start web proxy")
		return err
	} else {
		logger.Info("WebServer started.")
		return nil
	}
}

func (s *WebServer) Shutdown(ctx context.Context) {
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			logger.Error(err, "Failed to shutdown web proxy")
		} else {
			logger.Info("Proxy WebServer stopped.")
		}
	}
}

func (s *WebServer) registerHandler(handler WebHandler) {
	_, ok := lo.Find(s.handlers, func(item WebHandler) bool {
		return item.Path() == handler.Path() && item.Method() == handler.Method()
	})
	if ok {
		logger.Info("handler already registered", "Path", handler.Path(),
			"Method", handler.Method())
		return
	}
	logger.Info("WebServer register handler", "Path", handler.Path(),
		"Method", handler.Method())
	switch handler.Method() {
	case GET:
		s.r.GET(handler.Path(), handler.Handler)
	case POST:
		s.r.POST(handler.Path(), handler.Handler)
	case PUT:
		s.r.PUT(handler.Path(), handler.Handler)
	case DELETE:
		s.r.DELETE(handler.Path(), handler.Handler)
	}
	s.handlers = append(s.handlers, handler)
}

var _ WebHandler = &HealthCheckHandler{}

type HealthCheckHandler struct {
}

func (h *HealthCheckHandler) Path() string {
	return "/healthz"
}

func (h *HealthCheckHandler) Method() HttpMethod {
	return GET
}

func (h *HealthCheckHandler) Handler(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
