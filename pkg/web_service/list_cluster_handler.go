package web_service

import (
	"github.com/gin-gonic/gin"
	"github.com/pzhenzhou/elika/pkg/be_cluster"
	"net/http"
)

const (
	ListAllTenantsPath = "/list_cluster"
)

var _ WebHandler = (*ListAllTenantsHandler)(nil)

type ListAllTenantsHandler struct {
}

func (l *ListAllTenantsHandler) Path() string {
	return ListAllTenantsPath
}

func (l *ListAllTenantsHandler) Method() HttpMethod {
	return GET
}

func (l *ListAllTenantsHandler) Handler(ctx *gin.Context) {
	object, _ := ctx.Get(ClusterRegistryKey)
	backendDiscovery := object.(be_cluster.ClusterRegistry)
	ctx.JSON(http.StatusOK, ApiResponse{
		Code:    http.StatusOK,
		Message: "success",
		Data:    backendDiscovery.AllClusterInstances(),
	})
}
