// Package webui 内嵌后台前端构建产物，提供 SPA 静态服务。
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// dist 由 `pnpm build` 生成；仓库内保留占位 index.html，保证 go build 可用。
//
//go:embed all:dist
var embedded embed.FS

// Sub 返回 dist 根目录。
func Sub() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}

// Register 挂载 /admin 下的静态资源与 SPA 回落。
func Register(r gin.IRouter, base string) {
	sub, err := Sub()
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	stripped := http.StripPrefix(base, fileServer)

	serveIndex := func(c *gin.Context) {
		data, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			c.String(http.StatusOK, "后台界面尚未构建，请先执行 cd web && pnpm install && pnpm build")
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	}

	handler := func(c *gin.Context) {
		p := c.Param("filepath")
		clean := strings.TrimPrefix(path.Clean("/"+p), "/")

		if strings.HasPrefix(clean, "assets/") {
			if f, err := sub.Open(clean); err == nil {
				_ = f.Close()
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
				stripped.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		if clean != "" {
			if f, err := sub.Open(clean); err == nil {
				_ = f.Close()
				c.Header("Cache-Control", "public, max-age=3600")
				stripped.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		serveIndex(c)
	}

	r.GET(base, func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, base+"/") })
	r.GET(base+"/*filepath", handler)
	r.HEAD(base+"/*filepath", handler)
}
