package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/store"
	"github.com/starschedule/server/internal/store/postgres"
)

func deviceSummary(dev *model.Device) dto.DeviceSummary {
	return dto.DeviceSummary{
		ID:               dev.ID.String(),
		InstallID:        dev.InstallID,
		Platform:         dev.Platform,
		AppVersion:       dev.AppVersion,
		VersionCode:      dev.VersionCode,
		OSVersion:        dev.OSVersion,
		SDKInt:           dev.SDKInt,
		Arch:             dev.Arch,
		Status:           dev.Status,
		BanReason:        dev.BanReason,
		BannedAt:         utcPtr(dev.BannedAt),
		FirstSeenAt:      dev.FirstSeenAt.UTC(),
		LastSeenAt:       dev.LastSeenAt.UTC(),
		LaunchCount:      dev.LaunchCount,
		LastIP:           dev.LastIP,
		SuspectReinstall: dev.SuspectReinstall,
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

// AdminListDevices GET /api/v1/admin/devices
func (d *Deps) AdminListDevices(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	filter := postgres.DeviceFilter{
		Query:  c.Query("q"),
		Status: c.Query("status"),
		Limit:  limit,
		Cursor: c.Query("cursor"),
	}
	rows, next, hasMore, err := d.Store.ListDevices(c.Request.Context(), filter)
	if err != nil {
		if strings.Contains(err.Error(), "游标") {
			respond.InvalidArgument(c, "分页游标无效")
			return
		}
		d.Logger.Error("查询设备列表失败", "error", err)
		respond.Internal(c, "查询失败")
		return
	}
	total, _ := d.Store.CountDevices(c.Request.Context(), filter)

	items := make([]dto.DeviceSummary, 0, len(rows))
	for i := range rows {
		items = append(items, deviceSummary(&rows[i]))
	}
	c.JSON(http.StatusOK, respond.CursorPage[dto.DeviceSummary]{
		Items:         items,
		NextCursor:    next,
		HasMore:       hasMore,
		TotalEstimate: total,
	})
}

// AdminDeviceDetail GET /api/v1/admin/devices/:id
func (d *Deps) AdminDeviceDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "设备 ID 无效")
		return
	}
	ctx := c.Request.Context()
	dev, err := d.Store.GetDeviceByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.NotFound(c, "设备不存在")
			return
		}
		respond.Internal(c, "查询失败")
		return
	}
	activity, _ := d.Store.DeviceDailyActivity(ctx, id, 30, d.Loc.String())
	launches, _ := d.Store.RecentLaunches(ctx, id, 20)
	usage, _ := d.Store.UsageOn(ctx, d.Device.Today(), id)

	c.JSON(http.StatusOK, gin.H{
		"device":   deviceSummary(dev),
		"activity": activity,
		"launches": launches,
		"usage_today": gin.H{
			"request_count":     usage.RequestCount,
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"tokens":            usage.PromptTokens + usage.CompletionTokens,
			"estimated_count":   usage.EstimatedCount,
		},
	})
}

// AdminSetDeviceBan POST /api/v1/admin/devices/:id/ban
func (d *Deps) AdminSetDeviceBan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "设备 ID 无效")
		return
	}
	var req dto.BanRequest
	_ = c.ShouldBindJSON(&req)

	ctx := c.Request.Context()
	dev, err := d.Store.SetDeviceBanned(ctx, id, req.Banned, req.Reason)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.NotFound(c, "设备不存在")
			return
		}
		respond.Internal(c, "操作失败")
		return
	}
	if req.Banned {
		d.Redis.SetBanned(ctx, dev.ID.String())
	} else {
		d.Redis.ClearBanned(ctx, dev.ID.String())
	}

	action := "device.unban"
	if req.Banned {
		action = "device.ban"
	}
	d.Admin.Audit(ctx, adminFromAny(c), action, "device", dev.ID.String(),
		map[string]any{"reason": req.Reason}, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{"device": deviceSummary(dev)})
}
