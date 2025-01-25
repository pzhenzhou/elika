package web_service

import (
	"github.com/gin-gonic/gin"
	"github.com/pzhenzhou/elika/pkg/be_cluster"
	"net/http"
)

const (
	AddTenantPath = "/add_cluster"
)

var _ WebHandler = (*AddTenantHandler)(nil)

type AddTenantHandler struct{}

func (a *AddTenantHandler) Path() string {
	return AddTenantPath
}

func (a *AddTenantHandler) Method() HttpMethod {
	return POST
}

func (a *AddTenantHandler) Handler(ctx *gin.Context) {
	var request be_cluster.ClusterKey
	if err := ctx.ShouldBindBodyWithJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, ApiResponse{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}
	object, _ := ctx.Get(ClusterRegistryKey)
	backendDiscovery := object.(be_cluster.ClusterRegistry)
	err := backendDiscovery.AddCluster(&request)
	code := http.StatusOK
	if err != nil {
		code = http.StatusBadRequest
	} else {
		logger.Info("cluster added", "cluster", &request)
	}
	ctx.JSON(code, ApiResponse{
		Code:    code,
		Message: "cluster added",
	})
}
