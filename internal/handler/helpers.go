package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/middleware"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
)

func deviceFromAny(c *gin.Context) *model.Device   { return middleware.DeviceFrom(c) }
func adminFromAny(c *gin.Context) *model.AdminUser { return middleware.AdminFrom(c) }

type reqLogOptions struct {
	Stream       bool
	Usage        aiproxy.Usage
	Status       string
	HTTPStatus   int
	ErrorCode    string
	FirstTokenMS *int
	TotalMS      int
}

// writeRequestLog 落明细日志；即使客户端已断开也要写，故用脱离取消的 context。
func (d *Deps) writeRequestLog(c *gin.Context, caller chatCaller, snap *service.CatalogSnapshot, opt reqLogOptions) {
	providerID := snap.ProviderID
	l := postgres.RequestLog{
		ID:               uuid.Must(uuid.NewV7()),
		RequestID:        respond.RequestID(c),
		DeviceID:         caller.DeviceID,
		AdminID:          caller.AdminID,
		StatDate:         d.Device.Today(),
		ProviderID:       &providerID,
		Model:            snap.Alias,
		Stream:           opt.Stream,
		Source:           caller.Source,
		Status:           opt.Status,
		HTTPStatus:       opt.HTTPStatus,
		ErrorCode:        opt.ErrorCode,
		PromptTokens:     opt.Usage.PromptTokens,
		CompletionTokens: opt.Usage.CompletionTokens,
		TotalTokens:      opt.Usage.TotalTokens,
		Estimated:        opt.Usage.Estimated,
		FirstTokenMS:     opt.FirstTokenMS,
		TotalMS:          &opt.TotalMS,
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
	defer cancel()
	if err := d.Store.InsertRequestLog(ctx, l); err != nil {
		d.Logger.Warn("写入 AI 请求日志失败", "error", err, "request_id", l.RequestID)
	}
}
