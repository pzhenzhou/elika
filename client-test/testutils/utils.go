package testutils

import (
	"fmt"
	"github.com/pzhenzhou/elika/pkg/common"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var (
	Logger          = common.InitLogger().WithName("[Client-TEST]")
	BackendAddr     = "127.0.0.1:6379"
	ProxySrvAddr    = "127.0.0.1:6378"
	Username        = "nObPHzCQnwJ.admin"
	Password        = "admin"
	BackendPoolSize = -1
)

func GenerateKey(cmd string) string {
	timestamp := time.Now().UnixMilli()
	key := fmt.Sprintf("client_test_%s_%d", cmd, timestamp)
	return key
}

// BuildAndRunProxySrv eloq-proxy proxy. run : make build
func BuildAndRunProxySrv(backendAddr string) {
	cmd := exec.Command("make", "build")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error executing make build: %v\n", err)
		panic(err)
	}
	fmt.Printf("Output of make build:\n%s\n", string(output))
	_, b, _, _ := runtime.Caller(0)
	proxyBinary := filepath.Join(filepath.Dir(b), "../../bin/elika-proxy-srv")
	Logger.Info("Running eloq-proxy proxy", "RunCmd", proxyBinary)
	cmdArg := []string{
		"--router.type=static",
		fmt.Sprintf("--router.static-be=%s", backendAddr),
	}
	if BackendPoolSize > 0 {
		cmdArg = append(cmdArg, fmt.Sprintf("--backend-pool.max-size=%d", BackendPoolSize))
	}
	cmd = exec.Command(proxyBinary, cmdArg...)
	proxyLogFile := filepath.Join(filepath.Dir(b), "../logs/")
	err = os.MkdirAll(proxyLogFile, os.ModePerm)
	if err != nil {
		fmt.Printf("Error creating log file: %v\n", err)
		return
	}

	logFile, openErr := os.Create(filepath.Join(proxyLogFile, "elika-proxy-srv.log"))
	if openErr != nil {
		fmt.Printf("Error opening log file: %v\n", openErr)
		panic(openErr)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	startErr := cmd.Start()
	if startErr != nil {
		fmt.Printf("Error starting command: %v\n", startErr)
		panic(err)
	}
	Logger.Info("Started command", "PID", cmd.Process.Pid)
}
