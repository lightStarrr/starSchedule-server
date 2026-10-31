// Package metrics 暴露 Prometheus 指标。
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "按路由与状态码统计的请求数",
	}, []string{"method", "path", "status"})

	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "请求耗时分布",
		Buckets: []float64{0.01, 0.05, 0.1, 0.3, 0.6, 1, 3, 6, 12, 30, 60, 120, 300},
	}, []string{"method", "path"})

	AIRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_requests_total",
		Help: "AI 请求数",
	}, []string{"source", "model", "status"})

	AITokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_tokens_total",
		Help: "AI token 用量",
	}, []string{"model", "kind"})

	UpstreamFirstToken = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_first_token_seconds",
		Help:    "上游首字延迟",
		Buckets: []float64{0.2, 0.5, 1, 2, 4, 8, 16, 30},
	}, []string{"model"})
)

func Register() {
	prometheus.MustRegister(HTTPRequests, HTTPDuration, AIRequests, AITokens, UpstreamFirstToken)
}

// Middleware 用路由模板做 label，禁止把原始 URL 或 device_id 当标签。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		HTTPRequests.WithLabelValues(c.Request.Method, path, strconv.Itoa(c.Writer.Status())).Inc()
		HTTPDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}

func Handler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}
