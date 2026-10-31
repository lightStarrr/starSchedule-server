package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/respond"
)

// 允许后台热改的参数键；其余键一律拒绝，避免任意写。
var settingKeys = map[string]string{
	"ai.daily_token_quota":        "int",
	"ai.rpm_per_device":           "int",
	"ai.inflight_per_device":      "int",
	"ai.global_concurrency":       "int",
	"ai.max_output_tokens":        "int",
	"device.register_per_ip_hour": "int",
	"retention.launch_event_days": "int",
	"retention.ai_log_days":       "int",
}

// AdminGetSettings GET /api/v1/admin/settings
func (d *Deps) AdminGetSettings(c *gin.Context) {
	ctx := c.Request.Context()
	raw, err := d.Settings.Raw(ctx)
	if err != nil {
		respond.Internal(c, "读取配置失败")
		return
	}
	effective := d.Settings.Get(ctx)

	overrides := map[string]any{}
	for k, v := range raw {
		if _, ok := settingKeys[k]; ok {
			overrides[k] = v
		}
	}

	def := d.Settings.Defaults()
	c.JSON(http.StatusOK, gin.H{
		"overrides": overrides,
		"effective": gin.H{
			"ai.daily_token_quota":        effective.DailyTokenQuota,
			"ai.rpm_per_device":           effective.RPMPerDevice,
			"ai.inflight_per_device":      effective.InflightPerDevice,
			"ai.global_concurrency":       effective.GlobalConcurrency,
			"ai.max_output_tokens":        effective.MaxOutputTokens,
			"device.register_per_ip_hour": effective.RegisterPerIPHour,
			"retention.launch_event_days": effective.LaunchRetentionDays,
			"retention.ai_log_days":       effective.AILogRetentionDays,
		},
		"defaults": gin.H{
			"ai.daily_token_quota":        def.DailyTokenQuota,
			"ai.rpm_per_device":           def.RPMPerDevice,
			"ai.inflight_per_device":      def.InflightPerDevice,
			"ai.global_concurrency":       def.GlobalConcurrency,
			"ai.max_output_tokens":        def.MaxOutputTokens,
			"device.register_per_ip_hour": def.RegisterPerIPHour,
			"retention.launch_event_days": def.LaunchRetentionDays,
			"retention.ai_log_days":       def.AILogRetentionDays,
		},
	})
}

// AdminUpdateSettings PUT /api/v1/admin/settings
// 值传 null 表示恢复默认（删除覆盖行）。
func (d *Deps) AdminUpdateSettings(c *gin.Context) {
	var req dto.SettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	if len(req.Values) == 0 {
		respond.InvalidArgument(c, "values 不能为空")
		return
	}

	ctx := c.Request.Context()
	updates := map[string]any{}
	for k, v := range req.Values {
		if _, ok := settingKeys[k]; !ok {
			respond.InvalidArgument(c, "不支持的配置项："+k)
			return
		}
		if v == nil {
			if err := d.Store.DeleteSetting(ctx, k); err != nil {
				respond.Internal(c, "恢复默认值失败")
				return
			}
			continue
		}
		num, ok := toPositiveInt(v)
		if !ok {
			respond.InvalidArgument(c, "配置项 "+k+" 必须是非负整数")
			return
		}
		updates[k] = num
	}
	if len(updates) > 0 {
		if err := d.Settings.Update(ctx, updates); err != nil {
			respond.Internal(c, "保存失败")
			return
		}
	}
	d.Settings.Invalidate(ctx)

	d.Admin.Audit(ctx, adminFromAny(c), "settings.update", "settings", "", req.Values, c.ClientIP())
	d.AdminGetSettings(c)
}

func toPositiveInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 || n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return int64(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
