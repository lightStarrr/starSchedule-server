package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/respond"
)

// AdminStatsOverview GET /api/v1/admin/stats/overview
func (d *Deps) AdminStatsOverview(c *gin.Context) {
	ov, err := d.Stats.Overview(c.Request.Context())
	if err != nil {
		d.Logger.Error("统计总览失败", "error", err)
		respond.Internal(c, "统计失败")
		return
	}
	c.JSON(http.StatusOK, ov)
}

// AdminStatsTrend GET /api/v1/admin/stats/trend?days=30
func (d *Deps) AdminStatsTrend(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	points, err := d.Stats.Trend(c.Request.Context(), days)
	if err != nil {
		d.Logger.Error("统计趋势失败", "error", err)
		respond.Internal(c, "统计失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": points})
}

// AdminStatsDistribution GET /api/v1/admin/stats/distribution?dim=app_version&days=7
func (d *Deps) AdminStatsDistribution(c *gin.Context) {
	dim := c.DefaultQuery("dim", "app_version")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	items, err := d.Stats.Distribution(c.Request.Context(), dim, days)
	if err != nil {
		respond.InvalidArgument(c, "不支持的统计维度："+dim)
		return
	}
	c.JSON(http.StatusOK, gin.H{"dim": dim, "items": items})
}
