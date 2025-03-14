package common

import (
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"net"
	"strconv"
	"strings"
)

type WebServerConfig struct {
	EnablePprof bool `help:"Enable pprof for the web proxy" name:"pprof" default:"true"`
}

type BackendRouterConfig struct {
	LBType        string `help:"Type of the load balancer (e.g., round-robin, least-cluster, random)" name:"balancer" default:"random"`
	RouterType    string `help:"Type of the backend router (e.g., static, sync)" name:"type" required:"true"`
	StaticBackend string `help:"Address of the static backend (e.g., 127.0.0.1:6379)" name:"static-be"`
	CpAddr        string `help:"Address of the control plane" name:"cp-addr"`
}

func (r *BackendRouterConfig) StatisEndpoint() (string, int, error) {
	addr := r.StaticBackend
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid static backend address: %s", addr)
	}
	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid static backend port: %s", parts[1])
	}
	return host, port, nil
}

func (r *BackendRouterConfig) Validate() error {
	routerType := strings.ToLower(r.RouterType)
	switch routerType {
	case "static":
		if r.StaticBackend == "" {
			return fmt.Errorf("static backend address (--static-cluster) is required for router type: %s", r.RouterType)
		}
		if r.CpAddr != "" {
			return fmt.Errorf("control plane address (--cp-addr) should not cluster set for router type: %s", r.RouterType)
		}

	case "sync":
		if r.CpAddr == "" {
			return fmt.Errorf("control plane address (--cp-addr) is required for router type: %s", r.RouterType)
		}
		if r.StaticBackend != "" {
			return fmt.Errorf("static backend address (--static-cluster) should not cluster set for router type: %s", r.RouterType)
		}

	default:
		return fmt.Errorf("invalid router type: %s (must cluster 'static' or 'sync')", r.RouterType)
	}
	return nil
}

type BackendPoolConfig struct {
	IsFixed bool `help:"Fixed size backend pool" name:"fixed" default:"true"`
	MaxSize int  `help:"Maximum size of the backend pool" default:"30"`
	MaxIdle int  `help:"Maximum idle size of the backend pool" default:"10"`
}

type NodeConfig struct {
	NodeId    string `help:"Node identity" name:"id" default:"local_proxy"`
	Namespace string `help:"Namespace for the node" name:"namespace" default:"default"`
}

type MetricsConfig struct {
	EnableMetrics   bool   `help:"Enable metrics collection" name:"enable" default:"false"`
	MetricsPath     string `help:"Metrics path" name:"path" default:"/metrics"`
	MetricsSinkType string `help:"Metrics sink type. support prometheus and memory." name:"sink" default:"prometheus"`
}

type ProxyConfig struct {
	ProxyPort             int                 `help:"ProxyPort for the proxy proxy" name:"port" default:"6378"`
	ServicePort           int                 `help:"ServicePort for the proxy proxy. Port shared by the http and GRPC." name:"service-port" default:"7080"`
	MultiCore             bool                `help:"Enable multi-core support" default:"true"`
	CoreNum               int                 `help:"Number of cores to use" default:"0"`
	EnableTLS             bool                `help:"Enable TLS for the proxy proxy" default:"false"`
	EnableActiveUserTrace bool                `help:"Enable active user trace" name:"trace-active-user" default:"false"`
	BeConnPool            BackendPoolConfig   `embed:"" prefix:"backend-pool."`
	Router                BackendRouterConfig `embed:"" prefix:"router."`
	WebServer             WebServerConfig     `embed:"" prefix:"web-proxy."`
	Node                  NodeConfig          `embed:"" prefix:"node."`
	Metrics               MetricsConfig       `embed:"" prefix:"metrics."`
}

func (c *ProxyConfig) ServiceListener() net.Listener {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", c.ServicePort))
	if err != nil {
		panic(err)
	}
	return lis
}

func (c *ProxyConfig) Validate() error {
	if c.ProxyPort <= 0 {
		return fmt.Errorf("invalid port number: %d", c.ProxyPort)
	}
	return c.Router.Validate()
}

func (c *ProxyConfig) GNetOptions() []gnet.Option {
	var ops []gnet.Option
	if c.MultiCore {
		ops = append(ops, gnet.WithMulticore(true))
	}
	if c.CoreNum > 0 {
		ops = append(ops, gnet.WithNumEventLoop(c.CoreNum))
	}
	return ops
}
