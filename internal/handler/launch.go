package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
)

// ReportLaunch POST /api/v1/launches
// session_id 重复时同样返回 204（客户端重试是常态，不当作错误）。
func (d *Deps) ReportLaunch(c *gin.Context) {
	var req dto.LaunchRequest
	if !respond.BindJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		respond.InvalidArgument(c, "session_id 不能为空")
		return
	}

	dev := middleware.DeviceFrom(c)
	if dev == nil {
		respond.Unauthenticated(c, "缺少设备上下文")
		return
	}

	_, err := d.Launch.Report(c.Request.Context(), dev, service.LaunchInput{
		SessionID:   req.SessionID,
		LaunchedAt:  req.LaunchedAt,
		LaunchType:  req.LaunchType,
		AppVersion:  req.AppVersion,
		VersionCode: req.VersionCode,
		OSVersion:   req.OSVersion,
		SDKInt:      req.SDKInt,
		Arch:        req.Arch,
		Extra:       req.Extra,
	})
	if err != nil {
		d.Logger.Error("启动上报失败", "error", err, "device_id", dev.ID,
			"request_id", respond.RequestID(c))
		respond.Internal(c, "上报失败，请稍后重试")
		return
	}
	c.Status(http.StatusNoContent)
}
