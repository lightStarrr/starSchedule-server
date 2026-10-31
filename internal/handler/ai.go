package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/aiproxy"
	"github.com/starschedule/server/internal/metrics"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/service"
	redisc "github.com/starschedule/server/internal/store/redis"
)

// Models GET /api/v1/ai/models
func (d *Deps) AIModels(c *gin.Context) {
	ctx := c.Request.Context()
	snap, err := d.Catalog.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, service.ErrAINotConfigured) {
			respond.Error(c, http.StatusServiceUnavailable, respond.CodeAINotConfigured,
				"服务端尚未配置可用模型", nil)
			return
		}
		d.Logger.Error("读取模型配置失败", "error", err)
		respond.Internal(c, "读取配置失败")
		return
	}
	models, provider, err := d.Catalog.EnabledModels(ctx)
	if err != nil {
		respond.Internal(c, "读取模型列表失败")
		return
	}

	items := make([]gin.H, 0, len(models))
	for _, m := range models {
		items = append(items, gin.H{
			"alias":           m.Alias,
			"display_name":    m.DisplayName,
			"provider_name":   provider.Name,
			"context_window":  m.ContextWindow,
			"supports_stream": true,
			"is_current":      m.ID == snap.ModelUUID,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"current":     snap.Alias,
		"server_time": serverNow(),
	})
}

// AIConfig GET /api/v1/ai/config
func (d *Deps) AIConfig(c *gin.Context) {
	ctx := c.Request.Context()
	snap, err := d.Catalog.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, service.ErrAINotConfigured) {
			respond.Error(c, http.StatusServiceUnavailable, respond.CodeAINotConfigured,
				"服务端尚未配置可用模型", nil)
			return
		}
		respond.Internal(c, "读取配置失败")
		return
	}
	settings := d.Settings.Get(ctx)

	var used int64
	if dev := deviceFromAny(c); dev != nil {
		if row, err := d.Store.UsageOn(ctx, d.Device.Today(), dev.ID); err == nil {
			used = row.PromptTokens + row.CompletionTokens
		}
	}
	remaining := settings.DailyTokenQuota - used
	if remaining < 0 {
		remaining = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"model": gin.H{
			"alias":           snap.Alias,
			"display_name":    snap.DisplayName,
			"context_window":  snap.ContextWindow,
			"supports_stream": true,
		},
		"limits": gin.H{
			"rpm":               settings.RPMPerDevice,
			"inflight":          settings.InflightPerDevice,
			"max_output_tokens": settings.MaxOutputTokens,
			"max_input_bytes":   131072,
			"daily_token_quota": settings.DailyTokenQuota,
		},
		"usage": gin.H{
			"used_tokens":      used,
			"remaining_tokens": remaining,
			"request_count":    0,
			"reset_at":         d.resetAt().Format(time.RFC3339),
		},
		"server_time": serverNow(),
	})
}

// AIUsage GET /api/v1/ai/usage
func (d *Deps) AIUsage(c *gin.Context) {
	dev := deviceFromAny(c)
	if dev == nil {
		respond.Unauthenticated(c, "缺少设备上下文")
		return
	}
	ctx := c.Request.Context()
	settings := d.Settings.Get(ctx)
	row, err := d.Store.UsageOn(ctx, d.Device.Today(), dev.ID)
	if err != nil {
		respond.Internal(c, "读取用量失败")
		return
	}
	used := row.PromptTokens + row.CompletionTokens
	remaining := settings.DailyTokenQuota - used
	if remaining < 0 {
		remaining = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"date":             d.Device.Today().Format("2006-01-02"),
		"daily_quota":      settings.DailyTokenQuota,
		"used_tokens":      used,
		"remaining_tokens": remaining,
		"request_count":    row.RequestCount,
		"reset_at":         d.resetAt().Format(time.RFC3339),
	})
}

type chatCaller struct {
	DeviceID   *uuid.UUID
	AdminID    *uuid.UUID
	Source     string
	RateKey    string
	ApplyQuota bool
}

// AIChat POST /api/v1/ai/chat/completions
func (d *Deps) AIChat(c *gin.Context) {
	dev := deviceFromAny(c)
	if dev == nil {
		respond.Unauthenticated(c, "缺少设备上下文")
		return
	}
	var req aiproxy.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "请求体解析失败："+err.Error())
		return
	}
	deviceID := dev.ID
	d.handleChat(c, chatCaller{
		DeviceID:   &deviceID,
		Source:     "device",
		RateKey:    "device:" + dev.ID.String(),
		ApplyQuota: true,
	}, &req)
}

// AIPlayground POST /api/v1/admin/ai/playground（管理员试跑，不占设备配额）
func (d *Deps) AIPlayground(c *gin.Context) {
	admin := adminFromAny(c)
	var req aiproxy.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "请求体解析失败："+err.Error())
		return
	}
	var adminID *uuid.UUID
	rateKey := "admin:" + c.ClientIP()
	if admin != nil {
		id := admin.ID
		adminID = &id
		rateKey = "admin:" + id.String()
	}
	d.handleChat(c, chatCaller{
		AdminID:    adminID,
		Source:     "playground",
		RateKey:    rateKey,
		ApplyQuota: false,
	}, &req)
}

func (d *Deps) handleChat(c *gin.Context, caller chatCaller, req *aiproxy.ChatRequest) {
	ctx := c.Request.Context()
	requestID := respond.RequestID(c)

	settings := d.Settings.Get(ctx)
	if msg := req.Validate(settings.MaxOutputTokens, 131072); msg != "" {
		respond.InvalidArgument(c, msg)
		return
	}

	snap, err := d.Catalog.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, service.ErrAINotConfigured) {
			respond.Error(c, http.StatusServiceUnavailable, respond.CodeAINotConfigured,
				"服务端尚未配置可用模型", nil)
			return
		}
		respond.Internal(c, "读取模型配置失败")
		return
	}
	if req.Model != "" && req.Model != snap.Alias && req.Model != snap.ModelID {
		respond.Error(c, http.StatusBadRequest, respond.CodeModelNotAvailable,
			"指定的模型不可用，请重新获取配置", nil)
		return
	}

	// QPS：Redis 异常时 fail-open，仅记录。
	if allowed, retry, lerr := d.Redis.AllowFixedWindow(ctx,
		"rl:ai:"+caller.RateKey, settings.RPMPerDevice, time.Minute); lerr == nil && !allowed {
		c.Header("Retry-After", strconv.Itoa(retry))
		respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
			"请求过于频繁，请稍后重试", map[string]any{"retry_after": retry})
		return
	}

	// 并发闸门：lease 5 分钟，进程崩溃也不会永久占位。
	inflightKey := "inflight:ai:" + caller.RateKey
	if ok, ierr := d.Redis.AcquireInflight(ctx, inflightKey, settings.InflightPerDevice, 5*time.Minute); ierr == nil {
		if !ok {
			respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
				"并发请求过多，请等待当前对话结束", nil)
			return
		}
		defer d.Redis.ReleaseInflight(context.WithoutCancel(ctx), inflightKey)
	}

	promptEstimate := aiproxy.EstimateMessagesTokens(req.Messages)
	maxOutput := settings.MaxOutputTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 && *req.MaxTokens < maxOutput {
		maxOutput = *req.MaxTokens
	}

	var quotaKey string
	var charged int64
	if caller.ApplyQuota && caller.DeviceID != nil {
		quotaKey = redisc.QuotaKey(caller.DeviceID.String(), d.Device.Today().Format("2006-01-02"))
		charged = int64(promptEstimate + maxOutput)
		ttl := time.Until(d.resetAt()) + time.Hour
		allowed, used, qerr := d.Redis.PrechargeQuota(ctx, quotaKey, charged, settings.DailyTokenQuota, ttl)
		if qerr != nil {
			respond.Error(c, http.StatusServiceUnavailable, respond.CodeQuotaUnavailable,
				"额度服务暂不可用，请稍后重试", nil)
			return
		}
		if !allowed {
			c.Header("X-Quota-Remaining", "0")
			respond.Error(c, http.StatusTooManyRequests, respond.CodeAIQuotaExceeded,
				"今日 AI 额度已用完，可在额度重置后继续使用",
				map[string]any{
					"daily_quota":      settings.DailyTokenQuota,
					"used":             used,
					"reset_at":         d.resetAt().Format(time.RFC3339),
					"requested_tokens": charged,
				})
			return
		}
	}

	settled := false
	settle := func(prompt, completion int, estimated bool) {
		if settled {
			return
		}
		settled = true
		bg := context.WithoutCancel(ctx)
		if quotaKey != "" {
			actual := int64(prompt + completion)
			_ = d.Redis.SettleQuota(bg, quotaKey, actual-charged, time.Until(d.resetAt())+time.Hour)
		}
		if caller.DeviceID != nil {
			if err := d.Store.IncrUsage(bg, d.Device.Today(), *caller.DeviceID,
				int64(prompt), int64(completion), estimated, false); err != nil {
				d.Logger.Warn("写入 AI 用量失败", "error", err)
			}
		}
		metrics.AITokens.WithLabelValues(snap.Alias, "prompt").Add(float64(prompt))
		metrics.AITokens.WithLabelValues(snap.Alias, "completion").Add(float64(completion))
	}
	defer func() {
		if !settled && quotaKey != "" {
			// 未走到上游或提前失败：整笔退还预扣额度。
			_ = d.Redis.SettleQuota(context.WithoutCancel(ctx), quotaKey, -charged,
				time.Until(d.resetAt())+time.Hour)
		}
	}()

	apiKey, err := d.Catalog.APIKey(snap)
	if err != nil {
		d.Logger.Error("解密上游密钥失败", "error", err)
		respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError, "上游配置异常，请联系管理员", nil)
		return
	}

	started := time.Now()
	resp, err := d.AI.OpenChat(ctx, snap.BaseURL, apiKey,
		req.BuildUpstream(snap.ModelID, settings.MaxOutputTokens, req.Stream))
	if err != nil {
		d.writeRequestLog(c, caller, snap, reqLogOptions{
			Stream:     req.Stream,
			Status:     "error",
			HTTPStatus: mapUpstreamStatus(err),
			ErrorCode:  upstreamCode(err),
			TotalMS:    int(time.Since(started).Milliseconds()),
		})
		metrics.AIRequests.WithLabelValues(caller.Source, snap.Alias, "error").Inc()
		d.respondUpstreamError(c, err)
		return
	}
	defer resp.Body.Close()

	if !req.Stream {
		out, err := aiproxy.ReadNonStream(resp.Body, snap.Alias, promptEstimate)
		if err != nil {
			d.writeRequestLog(c, caller, snap, reqLogOptions{
				Status:     "error",
				HTTPStatus: http.StatusBadGateway,
				ErrorCode:  respond.CodeUpstreamError,
				TotalMS:    int(time.Since(started).Milliseconds()),
			})
			metrics.AIRequests.WithLabelValues(caller.Source, snap.Alias, "error").Inc()
			respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError, "上游响应异常，请稍后重试", nil)
			return
		}
		out.ID = requestID
		settle(out.Usage.PromptTokens, out.Usage.CompletionTokens, out.Usage.Estimated)
		total := int(time.Since(started).Milliseconds())
		d.writeRequestLog(c, caller, snap, reqLogOptions{
			Usage:      out.Usage,
			Status:     "ok",
			HTTPStatus: http.StatusOK,
			TotalMS:    total,
		})
		metrics.AIRequests.WithLabelValues(caller.Source, snap.Alias, "ok").Inc()
		c.JSON(http.StatusOK, gin.H{
			"id":         out.ID,
			"object":     out.Object,
			"created":    out.Created,
			"model":      snap.Alias,
			"choices":    out.Choices,
			"usage":      out.Usage,
			"request_id": requestID,
		})
		return
	}

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	res, relayErr := aiproxy.Relay(ctx, c.Writer, resp.Body, aiproxy.Meta{
		ID:        requestID,
		Model:     snap.Alias,
		Alias:     snap.Alias,
		RequestID: requestID,
	}, promptEstimate, maxOutput)

	usage := aiproxy.Usage{
		PromptTokens:     res.PromptTokens,
		CompletionTokens: res.CompletionTokens,
		TotalTokens:      res.PromptTokens + res.CompletionTokens,
		Estimated:        res.Estimated,
	}
	settle(usage.PromptTokens, usage.CompletionTokens, usage.Estimated)

	status, code := "ok", ""
	if relayErr != nil {
		status, code = "error", respond.CodeUpstreamError
		d.Logger.Warn("SSE 转发中断", "error", relayErr, "request_id", requestID)
	}
	if res.FirstTokenMS > 0 {
		metrics.UpstreamFirstToken.WithLabelValues(snap.Alias).Observe(float64(res.FirstTokenMS) / 1000)
	}
	metrics.AIRequests.WithLabelValues(caller.Source, snap.Alias, status).Inc()
	var firstToken *int
	if res.FirstTokenMS > 0 {
		ft := res.FirstTokenMS
		firstToken = &ft
	}
	d.writeRequestLog(c, caller, snap, reqLogOptions{
		Stream:       true,
		Usage:        usage,
		Status:       status,
		HTTPStatus:   http.StatusOK,
		ErrorCode:    code,
		FirstTokenMS: firstToken,
		TotalMS:      res.TotalMS,
	})
}

func (d *Deps) respondUpstreamError(c *gin.Context, err error) {
	var up *aiproxy.UpstreamError
	if errors.As(err, &up) {
		switch up.Kind {
		case "timeout":
			respond.Error(c, http.StatusGatewayTimeout, respond.CodeUpstreamTimeout,
				"模型响应超时，请重试", nil)
			return
		case "rate_limited":
			respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
				"上游服务繁忙，请稍后重试", nil)
			return
		}
		respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError,
			"上游服务异常，请稍后重试", nil)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	respond.Error(c, http.StatusGatewayTimeout, respond.CodeUpstreamTimeout, "上游服务超时", nil)
}

func mapUpstreamStatus(err error) int {
	var up *aiproxy.UpstreamError
	if errors.As(err, &up) {
		if up.Kind == "timeout" {
			return http.StatusGatewayTimeout
		}
		return http.StatusBadGateway
	}
	return http.StatusGatewayTimeout
}

func upstreamCode(err error) string {
	var up *aiproxy.UpstreamError
	if errors.As(err, &up) && up.Kind == "timeout" {
		return respond.CodeUpstreamTimeout
	}
	return respond.CodeUpstreamError
}
