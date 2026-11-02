package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
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
// 返回所有已启用（供应商也启用）的模型，客户端可任选其一发起对话。
func (d *Deps) AIModels(c *gin.Context) {
	ctx := c.Request.Context()
	entries, err := d.Catalog.ListEnabled(ctx)
	if err != nil {
		d.Logger.Error("读取模型列表失败", "error", err)
		respond.Internal(c, "读取模型列表失败")
		return
	}
	if len(entries) == 0 {
		respond.Error(c, http.StatusServiceUnavailable, respond.CodeAINotConfigured,
			"服务端尚未配置可用模型", nil)
		return
	}

	// 默认模型：客户端不传 model 时使用，取后台当前配置
	current := ""
	if snap, err := d.Catalog.Snapshot(ctx); err == nil {
		current = snap.Alias
	}

	settings := d.Settings.Get(ctx)
	items := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		items = append(items, gin.H{
			"alias":           e.Alias,
			"display_name":    e.DisplayName,
			"provider_name":   e.ProviderName,
			"context_window":  e.ContextWindow,
			"supports_stream": true,
			"is_default":      e.Alias == current,
			"reasoning": reasoningJSON(e.ReasoningMode, e.ReasoningEfforts,
				reasoningDefaultFor(e.ReasoningMode, e.ReasoningEfforts, settings.ReasoningEffort)),
		})
	}
	// ETag 只覆盖「内容」部分（不含 server_time），命中时返回 304，
	// 客户端仍可从 X-Server-Time 头拿到新的校时时间。
	raw, err := json.Marshal(gin.H{"items": items, "default": current})
	if err != nil {
		respond.Internal(c, "生成响应失败")
		return
	}
	sum := sha256.Sum256(raw)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, max-age=60")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	body, err := json.Marshal(gin.H{
		"items":       items,
		"default":     current,
		"server_time": serverNow(),
	})
	if err != nil {
		respond.Internal(c, "生成响应失败")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
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
	var requestCount int64
	if dev := deviceFromAny(c); dev != nil {
		if row, err := d.Store.UsageOn(ctx, d.Device.Today(), dev.ID); err == nil {
			used = row.PromptTokens + row.CompletionTokens
			requestCount = row.RequestCount
		}
	}
	remaining := settings.RemainingTokens(used)

	c.JSON(http.StatusOK, gin.H{
		"model": gin.H{
			"alias":           snap.Alias,
			"display_name":    snap.DisplayName,
			"context_window":  snap.ContextWindow,
			"supports_stream": true,
			"reasoning": reasoningJSON(snap.ReasoningMode, snap.ReasoningEfforts,
				reasoningDefaultFor(snap.ReasoningMode, snap.ReasoningEfforts, settings.ReasoningEffort)),
		},
		"limits": gin.H{
			"quota_unlimited":   settings.QuotaUnlimited,
			"rpm":               settings.RPMPerDevice,
			"inflight":          settings.InflightPerDevice,
			"max_output_tokens": settings.MaxOutputTokens,
			"max_input_bytes":   131072,
			"daily_token_quota": settings.DailyTokenQuota,
		},
		"usage": gin.H{
			"used_tokens":      used,
			"remaining_tokens": remaining,
			"request_count":    requestCount,
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
	remaining := settings.RemainingTokens(used)
	c.JSON(http.StatusOK, gin.H{
		"date":             d.Device.Today().Format("2006-01-02"),
		"daily_quota":      settings.DailyTokenQuota,
		"quota_unlimited":  settings.QuotaUnlimited,
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
	// AllowModelOverride 允许客户端及试跑按 alias 选择已启用模型。
	AllowModelOverride bool
}

// AIChat POST /api/v1/ai/chat/completions
func (d *Deps) AIChat(c *gin.Context) {
	dev := deviceFromAny(c)
	if dev == nil {
		respond.Unauthenticated(c, "缺少设备上下文")
		return
	}
	var req aiproxy.ChatRequest
	if !respond.BindJSON(c, &req) {
		return
	}
	deviceID := dev.ID
	d.handleChat(c, chatCaller{
		DeviceID:           &deviceID,
		Source:             "device",
		RateKey:            "device:" + dev.ID.String(),
		ApplyQuota:         true,
		AllowModelOverride: true,
	}, &req)
}

// AIPlayground POST /api/v1/admin/ai/playground（管理员试跑，不占设备配额）
func (d *Deps) AIPlayground(c *gin.Context) {
	admin := adminFromAny(c)
	var req aiproxy.ChatRequest
	if !respond.BindJSON(c, &req) {
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
		AdminID:            adminID,
		Source:             "playground",
		RateKey:            rateKey,
		ApplyQuota:         false,
		AllowModelOverride: true,
	}, &req)
}

func (d *Deps) handleChat(c *gin.Context, caller chatCaller, req *aiproxy.ChatRequest) {
	ctx := c.Request.Context()
	requestID := respond.RequestID(c)

	settings := d.Settings.Get(ctx)
	const maxInputBytes = 131072
	if err := req.Validate(settings.MaxOutputTokens, maxInputBytes); err != nil {
		if errors.Is(err, aiproxy.ErrInputTooLarge) {
			respond.Error(c, http.StatusRequestEntityTooLarge, respond.CodePayloadTooLarge,
				err.Error(), map[string]any{"max_input_bytes": maxInputBytes})
			return
		}
		respond.InvalidArgument(c, err.Error())
		return
	}

	var snap *service.CatalogSnapshot
	var err error
	if caller.AllowModelOverride && req.Model != "" {
		// 同一个模型同时只会有一家供应商启用，alias 足以唯一定位
		snap, err = d.Catalog.SnapshotByAlias(ctx, req.Model)
	} else {
		snap, err = d.Catalog.Snapshot(ctx)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAINotConfigured):
			respond.Error(c, http.StatusServiceUnavailable, respond.CodeAINotConfigured,
				"服务端尚未配置可用模型", nil)
		case errors.Is(err, service.ErrModelNotFound):
			respond.Error(c, http.StatusBadRequest, respond.CodeModelNotAvailable,
				"指定的模型不可用，请重新获取配置", nil)
		default:
			respond.Internal(c, "读取模型配置失败")
		}
		return
	}
	if !caller.AllowModelOverride && req.Model != "" && req.Model != snap.Alias {
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
			c.Header("Retry-After", "1")
			respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
				"并发请求过多，请等待当前对话结束", map[string]any{"retry_after": 1})
			return
		}
		defer d.Redis.ReleaseInflight(context.WithoutCancel(ctx), inflightKey)
	}

	promptEstimate := aiproxy.EstimateMessagesTokens(req.Messages)
	maxOutput := settings.MaxOutputTokens

	// 思考强度：只有探测为 effort 的模型才接受，其余明确拒绝而不是静默忽略
	reasoningEffort := ""
	switch snap.ReasoningMode {
	case "effort":
		if req.ReasoningEffort == "" {
			// 没传就用全局默认，但必须收敛到本模型支持的档位：
			// 直接下发越界值会被上游拒掉，静默丢弃又与 /ai/models 声称的默认值矛盾
			reasoningEffort = aiproxy.ClampReasoningEffort(
				settings.ReasoningEffort, snap.ReasoningEfforts,
			)
		} else if len(snap.ReasoningEfforts) > 0 &&
			!slices.Contains(snap.ReasoningEfforts, req.ReasoningEffort) {
			// 显式指定了本模型没有的档位：明确报错，不偷偷改档
			respond.InvalidArgument(c, "该模型不支持所选思考强度")
			return
		} else {
			reasoningEffort = req.ReasoningEffort
		}
	case "fixed":
		if req.ReasoningEffort != "" {
			respond.InvalidArgument(c, "该模型固定带思考，无法设置思考强度")
			return
		}
	default:
		if req.ReasoningEffort != "" {
			respond.InvalidArgument(c, "该模型不支持设置思考强度")
			return
		}
	}

	var quotaKey string
	var charged int64
	usageDate := d.Device.Today()
	quotaReset := usageDate.AddDate(0, 0, 1)
	// 不限量期间也累计计费缓存，关闭开关后继续按当天总用量限制。
	if caller.ApplyQuota && caller.DeviceID != nil && settings.QuotaUnlimited {
		quotaKey = redisc.QuotaKey(caller.DeviceID.String(), usageDate.Format("2006-01-02"))
	}
	// 日额度由独立的不限量开关控制，频率和并发限制保持独立。
	if caller.ApplyQuota && caller.DeviceID != nil && !settings.QuotaUnlimited {
		quotaKey = redisc.QuotaKey(caller.DeviceID.String(), usageDate.Format("2006-01-02"))
		charged = int64(promptEstimate + maxOutput)
		ttl := time.Until(quotaReset) + time.Hour
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
					"quota_unlimited":  settings.QuotaUnlimited,
					"used":             used,
					"reset_at":         quotaReset.Format(time.RFC3339),
					"requested_tokens": charged,
				})
			return
		}
	}

	settled := false
	settle := func(usage aiproxy.Usage, isError bool) {
		if settled {
			return
		}
		settled = true
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if quotaKey != "" {
			actual := int64(usage.PromptTokens + usage.CompletionTokens)
			if err := d.Redis.SettleQuota(bg, quotaKey, actual-charged, time.Until(quotaReset)+time.Hour); err != nil {
				d.Logger.Warn("结算 AI 额度失败", "error", err, "request_id", requestID)
			}
		}
		if caller.DeviceID != nil {
			if err := d.Store.IncrUsage(bg, usageDate, *caller.DeviceID,
				int64(usage.PromptTokens), int64(usage.CompletionTokens), usage.Estimated, isError); err != nil {
				d.Logger.Warn("写入 AI 用量失败", "error", err)
			}
		}
		metrics.AITokens.WithLabelValues(snap.Alias, "prompt").Add(float64(usage.PromptTokens))
		metrics.AITokens.WithLabelValues(snap.Alias, "completion").Add(float64(usage.CompletionTokens))
	}
	defer func() {
		if !settled {
			// 提前失败：整笔退还预扣额度，同时记录一次失败请求。
			settle(aiproxy.Usage{}, true)
		}
	}()

	apiKey, err := d.Catalog.APIKey(snap)
	if err != nil {
		d.Logger.Error("解密上游密钥失败", "error", err)
		respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError, "上游配置异常，请联系管理员", nil)
		return
	}

	started := time.Now()
	timeout := time.Duration(snap.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	// 同一个截止时间覆盖连接、响应头和响应体读取，也适用于 SSE。
	upstreamCtx, cancelUpstream := context.WithTimeout(ctx, timeout)
	defer cancelUpstream()
	resp, err := d.AI.OpenChat(upstreamCtx, snap.BaseURL, apiKey,
		req.BuildUpstream(snap.ModelID, maxOutput, req.Stream, reasoningEffort))
	if err != nil {
		settle(aiproxy.Usage{}, true)
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
			settle(aiproxy.Usage{}, true)
			status, code, message := http.StatusBadGateway, respond.CodeUpstreamError, "上游响应异常，请稍后重试"
			if errors.Is(upstreamCtx.Err(), context.DeadlineExceeded) {
				status, code, message = http.StatusGatewayTimeout, respond.CodeUpstreamTimeout, "模型响应超时，请重试"
			}
			d.writeRequestLog(c, caller, snap, reqLogOptions{
				Status:     "error",
				HTTPStatus: status,
				ErrorCode:  code,
				TotalMS:    int(time.Since(started).Milliseconds()),
			})
			metrics.AIRequests.WithLabelValues(caller.Source, snap.Alias, "error").Inc()
			respond.Error(c, status, code, message, nil)
			return
		}
		out.ID = requestID
		settle(out.Usage, false)
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

	res, relayErr := aiproxy.Relay(upstreamCtx, c.Writer, resp.Body, aiproxy.Meta{
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
	settle(usage, relayErr != nil)

	status, code := "ok", ""
	if relayErr != nil {
		status, code = "error", upstreamCode(relayErr)
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
	if errors.Is(err, context.Canceled) {
		return
	}
	var up *aiproxy.UpstreamError
	if errors.As(err, &up) {
		switch up.Kind {
		case "timeout":
			respond.Error(c, http.StatusGatewayTimeout, respond.CodeUpstreamTimeout,
				"模型响应超时，请重试", nil)
			return
		case "rate_limited":
			retry := up.RetryAfter
			if retry <= 0 {
				retry = 3
			}
			c.Header("Retry-After", strconv.Itoa(retry))
			respond.Error(c, http.StatusTooManyRequests, respond.CodeRateLimited,
				"上游服务繁忙，请稍后重试", map[string]any{"retry_after": retry})
			return
		}
		respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError,
			"上游服务异常，请稍后重试", nil)
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
	if errors.Is(err, context.DeadlineExceeded) {
		return respond.CodeUpstreamTimeout
	}
	var up *aiproxy.UpstreamError
	if errors.As(err, &up) && up.Kind == "timeout" {
		return respond.CodeUpstreamTimeout
	}
	return respond.CodeUpstreamError
}
