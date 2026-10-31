package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/starschedule/server/internal/store/postgres"
)

// Healthz 只证明进程活着，不打依赖。
func (d *Deps) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().UTC()})
}

// Readyz 检查 PG 与 Redis；任一不可用即返回 503（AI 配额依赖 Redis fail-closed）。
func (d *Deps) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	checks := gin.H{}
	ready := true

	if err := postgres.Ping(ctx, d.Store.Pool()); err != nil {
		checks["postgres"] = "down: " + err.Error()
		ready = false
	} else {
		checks["postgres"] = "up"
	}

	if err := d.Redis.Ping(ctx); err != nil {
		checks["redis"] = "down: " + err.Error()
		ready = false
	} else {
		checks["redis"] = "up"
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"ready": ready, "checks": checks})
}

func (d *Deps) VersionInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": d.Version})
}
