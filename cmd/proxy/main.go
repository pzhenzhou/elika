package main

import (
	"context"
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/pzhenzhou/elika/pkg/common"
	"github.com/pzhenzhou/elika/pkg/proxy"
	"github.com/pzhenzhou/elika/pkg/web_service"
	cmux2 "github.com/soheilhy/cmux"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	logger   = common.InitLogger().WithName("main")
	proxyCfg common.ProxyConfig
)

func main() {
	ctx := kong.Parse(&proxyCfg)
	if err := proxyCfg.Validate(); err != nil {
		ctx.FatalIfErrorf(err)
	}
	fmt.Print(proxy.Banner)
	logger.Info("ElikaProxyServer ", "Config", proxyCfg)
	SetupAllServer()
}

func SetupAllServer() {
	srvListener := proxyCfg.ServiceListener()
	m := cmux2.New(srvListener)

	httpSrv := web_service.NewWebServer(&proxyCfg)
	proxySrv := proxy.NewElikaProxy(&proxyCfg)

	signChan := make(chan os.Signal, 1)
	signal.Notify(signChan, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM)
	errChan := make(chan error, 1)
	// start proxy tcp proxy
	go func() {
		if err := proxySrv.Start(); err != nil {
			errChan <- err
		}
	}()
	// start http proxy
	go func() {
		if err := httpSrv.Start(m); err != nil {
			errChan <- err
		}
	}()

	go func() {
		logger.Info("Starting cmux proxy...", "ServiceAddr", srvListener.Addr())
		if err := m.Serve(); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		logger.Error(err, "An error occurred when the cluster started.")
		os.Exit(-1)
	case sig := <-signChan:
		logger.Info("Received signal, shutting down...", "Sigs", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		httpSrv.Shutdown(ctx)
		proxySrv.Shutdown(ctx)
	}
}
