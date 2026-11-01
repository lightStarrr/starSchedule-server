package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/crypto"
	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/respond"
	"github.com/starschedule/server/internal/store"
	"github.com/starschedule/server/internal/store/postgres"
)

func (d *Deps) providerJSON(p *model.Provider) gin.H {
	masked := "****"
	if key, err := d.Catalog.Cipher().Decrypt(p.APIKeyCipher); err == nil {
		masked = crypto.Mask(key)
	}
	return gin.H{
		"id":               p.ID.String(),
		"name":             p.Name,
		"base_url":         p.BaseURL,
		"api_key_masked":   masked,
		"enabled":          p.Enabled,
		"sort_order":       p.SortOrder,
		"timeout_seconds":  p.TimeoutSeconds,
		"last_check_at":    p.LastCheckAt,
		"last_check_ok":    p.LastCheckOK,
		"last_check_error": p.LastCheckError,
		"created_at":       p.CreatedAt.UTC(),
	}
}

func modelJSON(m *model.ProviderModel) gin.H {
	return gin.H{
		"id":             m.ID.String(),
		"provider_id":    m.ProviderID.String(),
		"model_id":       m.ModelID,
		"alias":          m.Alias,
		"display_name":   m.DisplayName,
		"context_window": m.ContextWindow,
		"source":         m.Source,
		"enabled":        m.Enabled,
		"fetched_at":     m.FetchedAt.UTC(),
	}
}

// AdminCatalog GET /api/v1/admin/ai/catalog
// 返回所有可调用的模型（供应商与模型都启用），以及当前生效的那个。
func (d *Deps) AdminCatalog(c *gin.Context) {
	ctx := c.Request.Context()
	entries, err := d.Catalog.ListEnabled(ctx)
	if err != nil {
		respond.Internal(c, "查询模型失败")
		return
	}
	current := gin.H{"id": "", "alias": ""}
	if snap, err := d.Catalog.Snapshot(ctx); err == nil {
		current = gin.H{"id": snap.ModelUUID.String(), "alias": snap.Alias}
	}
	c.JSON(http.StatusOK, gin.H{"items": entries, "active": current})
}

// ensureDefaultModel 在没有默认模型时，把刚启用的模型记为默认，
// 作为客户端不指定 model 时的兜底，避免必须手动配置才能用。
func (d *Deps) ensureDefaultModel(ctx context.Context, m *model.ProviderModel) {
	if m == nil || !m.Enabled {
		return
	}
	if _, err := d.Store.GetActive(ctx); err == nil {
		return
	}
	p, err := d.Store.GetProvider(ctx, m.ProviderID)
	if err != nil || !p.Enabled {
		return
	}
	if err := d.Store.SetActive(ctx, m.ProviderID, m.ID); err == nil {
		d.Catalog.Invalidate(ctx)
	}
}

// AdminListProviders GET /api/v1/admin/ai/providers
func (d *Deps) AdminListProviders(c *gin.Context) {
	ctx := c.Request.Context()
	providers, err := d.Store.ListProviders(ctx)
	if err != nil {
		respond.Internal(c, "查询供应商失败")
		return
	}
	items := make([]gin.H, 0, len(providers))
	for i := range providers {
		items = append(items, d.providerJSON(&providers[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AdminCreateProvider POST /api/v1/admin/ai/providers
func (d *Deps) AdminCreateProvider(c *gin.Context) {
	var req dto.ProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	if req.APIKey == "" {
		respond.InvalidArgument(c, "api_key 不能为空")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	p, err := d.Catalog.CreateProvider(c.Request.Context(), serviceProviderPayload(req, enabled))
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			respond.Error(c, http.StatusConflict, respond.CodeInvalidArgument, "供应商名称已存在", nil)
			return
		}
		d.Logger.Error("创建供应商失败", "error", err)
		respond.Internal(c, "创建失败")
		return
	}
	d.Admin.Audit(c.Request.Context(), adminFromAny(c), "ai.provider.create", "provider", p.ID.String(),
		map[string]any{"name": p.Name}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"provider": d.providerJSON(p)})
}

// AdminUpdateProvider PATCH /api/v1/admin/ai/providers/:id
func (d *Deps) AdminUpdateProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "供应商 ID 无效")
		return
	}
	var req dto.ProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	p, err := d.Catalog.UpdateProvider(c.Request.Context(), id, serviceProviderPayload(req, enabled), req.APIKey != "")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.NotFound(c, "供应商不存在")
			return
		}
		d.Logger.Error("更新供应商失败", "error", err)
		respond.Internal(c, "更新失败")
		return
	}
	d.Admin.Audit(c.Request.Context(), adminFromAny(c), "ai.provider.update", "provider", p.ID.String(),
		map[string]any{"name": p.Name}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"provider": d.providerJSON(p)})
}

// AdminDeleteProvider DELETE /api/v1/admin/ai/providers/:id
func (d *Deps) AdminDeleteProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "供应商 ID 无效")
		return
	}
	if err := d.Store.DeleteProvider(c.Request.Context(), id); err != nil {
		respond.Internal(c, "删除失败")
		return
	}
	d.Catalog.Invalidate(c.Request.Context())
	d.Admin.Audit(c.Request.Context(), adminFromAny(c), "ai.provider.delete", "provider", id.String(), nil, c.ClientIP())
	c.Status(http.StatusNoContent)
}

// AdminTestProvider POST /api/v1/admin/ai/providers/:id/test
func (d *Deps) AdminTestProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "供应商 ID 无效")
		return
	}
	ok, msg := d.Catalog.TestProvider(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{"ok": ok, "error": msg})
}

// AdminSyncModels POST /api/v1/admin/ai/providers/:id/sync-models
func (d *Deps) AdminSyncModels(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "供应商 ID 无效")
		return
	}
	res, err := d.Catalog.SyncModels(c.Request.Context(), id)
	if err != nil {
		d.Logger.Warn("同步模型失败", "error", err, "provider_id", id)
		respond.Error(c, http.StatusBadGateway, respond.CodeUpstreamError, "同步失败："+err.Error(), nil)
		return
	}

	models, _ := d.Store.ListModels(c.Request.Context(), id)
	items := make([]gin.H, 0, len(models))
	missing := make([]gin.H, 0)
	for i := range models {
		items = append(items, modelJSON(&models[i]))
		// 只把「上游同步来的、这次没再出现」的模型列为待清理，手动添加的不动
		if models[i].Source == "fetched" && !res.UpstreamIDs[models[i].ModelID] {
			missing = append(missing, gin.H{
				"id":           models[i].ID.String(),
				"model_id":     models[i].ModelID,
				"alias":        models[i].Alias,
				"display_name": models[i].DisplayName,
			})
		}
	}

	d.Admin.Audit(c.Request.Context(), adminFromAny(c), "ai.models.sync", "provider", id.String(),
		map[string]any{"synced": res.Synced, "missing": len(missing)}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"synced": res.Synced, "items": items, "missing": missing})
}

// AdminGetActive GET /api/v1/admin/ai/active
func (d *Deps) AdminGetActive(c *gin.Context) {
	snap, err := d.Catalog.Snapshot(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"active": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"active": gin.H{
			"provider_id":    snap.ProviderID.String(),
			"provider_name":  snap.ProviderName,
			"model_id":       snap.ModelUUID.String(),
			"model":          snap.ModelID,
			"alias":          snap.Alias,
			"display_name":   snap.DisplayName,
			"context_window": snap.ContextWindow,
		},
	})
}

// AdminListModels GET /api/v1/admin/ai/models?provider_id=
func (d *Deps) AdminListModels(c *gin.Context) {
	ctx := c.Request.Context()
	entries, err := d.Store.ListAllModels(ctx)
	if err != nil {
		respond.Internal(c, "查询模型失败")
		return
	}

	filter := c.Query("provider_id")
	items := make([]gin.H, 0, len(entries))
	for _, e := range entries {
		if filter != "" && e.ProviderID.String() != filter {
			continue
		}
		items = append(items, gin.H{
			"id":             e.ModelUUID.String(),
			"provider_id":    e.ProviderID.String(),
			"provider_name":  e.ProviderName,
			"model_id":       e.ModelID,
			"alias":          e.Alias,
			"display_name":   e.DisplayName,
			"context_window": e.ContextWindow,
			"source":         e.Source,
			"enabled":        e.Enabled,
			"fetched_at":     e.FetchedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// AdminSetModelAvailability PUT /api/v1/admin/ai/models/availability
// 以模型为单位启停：开启时只让指定供应商那一行可用，其余供应商的同名模型关闭。
func (d *Deps) AdminSetModelAvailability(c *gin.Context) {
	var req dto.ModelAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	ctx := c.Request.Context()

	rows, err := d.Store.ListModelsByModelID(ctx, req.ModelID)
	if err != nil {
		respond.Internal(c, "查询模型失败")
		return
	}
	if len(rows) == 0 {
		respond.NotFound(c, "模型不存在")
		return
	}

	var target uuid.UUID
	if req.Enabled {
		switch {
		case req.ProviderID != nil:
			for _, r := range rows {
				if r.ProviderID.String() == *req.ProviderID {
					target = r.ProviderID
					break
				}
			}
			if target == uuid.Nil {
				respond.InvalidArgument(c, "该供应商下没有这个模型")
				return
			}
		default:
			for _, r := range rows {
				if r.Enabled {
					target = r.ProviderID
					break
				}
			}
			if target == uuid.Nil {
				target = rows[0].ProviderID
			}
		}
		p, err := d.Store.GetProvider(ctx, target)
		if err != nil || !p.Enabled {
			respond.InvalidArgument(c, "该供应商已停用，请先启用供应商")
			return
		}
	}

	for _, r := range rows {
		want := req.Enabled && r.ProviderID == target
		if r.Enabled == want {
			continue
		}
		enabled := want
		if _, err := d.Store.UpdateModel(ctx, r.ID, postgres.ModelPatch{Enabled: &enabled}); err != nil {
			respond.Internal(c, "更新模型失败")
			return
		}
	}
	d.Catalog.Invalidate(ctx)

	providerID := ""
	if target != uuid.Nil {
		providerID = target.String()
	}
	d.Admin.Audit(ctx, adminFromAny(c), "ai.models.availability", "model", req.ModelID,
		map[string]any{"enabled": req.Enabled, "provider_id": providerID}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"model_id": req.ModelID, "enabled": req.Enabled, "provider_id": providerID})
}

// AdminCreateModel POST /api/v1/admin/ai/models
func (d *Deps) AdminCreateModel(c *gin.Context) {
	var req dto.ModelCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	providerID, _ := uuid.Parse(req.ProviderID)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	m, err := d.Store.CreateModel(c.Request.Context(), postgresModelInput(req, providerID, enabled))
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			respond.Error(c, http.StatusConflict, respond.CodeInvalidArgument, "该模型或别名已存在", nil)
			return
		}
		respond.Internal(c, "创建失败")
		return
	}
	d.ensureDefaultModel(c.Request.Context(), m)
	c.JSON(http.StatusOK, gin.H{"model": modelJSON(m)})
}

// AdminUpdateModel PATCH /api/v1/admin/ai/models/:id
func (d *Deps) AdminUpdateModel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "模型 ID 无效")
		return
	}
	var req dto.ModelPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	m, err := d.Store.UpdateModel(c.Request.Context(), id,
		postgresModelPatch(req))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.NotFound(c, "模型不存在")
			return
		}
		if errors.Is(err, store.ErrConflict) {
			respond.Error(c, http.StatusConflict, respond.CodeInvalidArgument, "别名已被占用", nil)
			return
		}
		respond.Internal(c, "更新失败")
		return
	}
	d.Catalog.Invalidate(c.Request.Context())
	d.ensureDefaultModel(c.Request.Context(), m)
	c.JSON(http.StatusOK, gin.H{"model": modelJSON(m)})
}

// AdminBulkDeleteModels POST /api/v1/admin/ai/models/bulk-delete
func (d *Deps) AdminBulkDeleteModels(c *gin.Context) {
	var req dto.BulkDeleteModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.InvalidArgument(c, "参数校验失败："+err.Error())
		return
	}
	ctx := c.Request.Context()
	deleted := 0
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		if err := d.Store.DeleteModel(ctx, id); err == nil {
			deleted++
		}
	}
	d.Catalog.Invalidate(ctx)
	d.Admin.Audit(ctx, adminFromAny(c), "ai.models.delete", "model", "", map[string]any{"count": deleted}, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

// AdminDeleteModel DELETE /api/v1/admin/ai/models/:id
func (d *Deps) AdminDeleteModel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.InvalidArgument(c, "模型 ID 无效")
		return
	}
	if err := d.Store.DeleteModel(c.Request.Context(), id); err != nil {
		respond.Internal(c, "删除失败")
		return
	}
	d.Catalog.Invalidate(c.Request.Context())
	c.Status(http.StatusNoContent)
}

// AdminAIUsage GET /api/v1/admin/ai/usage?from=&to=&group_by=day|device
func (d *Deps) AdminAIUsage(c *gin.Context) {
	ctx := c.Request.Context()
	from, to := d.usageRange(c)
	groupBy := c.DefaultQuery("group_by", "day")
	if groupBy == "device" {
		items, err := d.Store.UsageByDevice(ctx, from, to, 20)
		if err != nil {
			respond.Internal(c, "查询失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"group_by": "device", "items": items})
		return
	}
	items, err := d.Store.UsageByDay(ctx, from, to)
	if err != nil {
		respond.Internal(c, "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"group_by": "day", "items": items})
}

func (d *Deps) usageRange(c *gin.Context) (time.Time, time.Time) {
	to := d.Device.Today()
	from := to.AddDate(0, 0, -6)
	if v := c.Query("from"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, d.Loc); err == nil {
			from = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, d.Loc); err == nil {
			to = t
		}
	}
	return from, to
}

// AdminAuditLogs GET /api/v1/admin/audit
func (d *Deps) AdminAuditLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := d.Store.ListAudit(c.Request.Context(), limit)
	if err != nil {
		respond.Internal(c, "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
