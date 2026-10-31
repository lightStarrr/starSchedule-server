// Package redis 封装缓存、限流与配额。
package redis

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrUnavailable 表示 Redis 已被熔断或不可达，调用方需走降级分支。
var ErrUnavailable = errors.New("redis 不可用")

type Client struct {
	rdb       *redis.Client
	fails     atomic.Int32
	openUntil atomic.Int64
}

func New(addr, password string, db int) *Client {
	return &Client{rdb: redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  400 * time.Millisecond,
		WriteTimeout: 400 * time.Millisecond,
		PoolSize:     20,
		MinIdleConns: 2,
	})}
}

func (c *Client) Raw() *redis.Client { return c.rdb }

func (c *Client) Close() error { return c.rdb.Close() }

func (c *Client) Ping(ctx context.Context) error {
	err := c.rdb.Ping(ctx).Err()
	c.record(err)
	return err
}

// Available 为 false 时调用方应直接降级，避免请求被 Redis 拖慢。
func (c *Client) Available() bool {
	return time.Now().UnixNano() >= c.openUntil.Load()
}

// record 是极简熔断：连续 5 次失败即断开 30 秒。
func (c *Client) record(err error) {
	if err == nil {
		c.fails.Store(0)
		return
	}
	if c.fails.Add(1) >= 5 {
		c.openUntil.Store(time.Now().Add(30 * time.Second).UnixNano())
		c.fails.Store(0)
	}
}

func (c *Client) gate() error {
	if !c.Available() {
		return ErrUnavailable
	}
	return nil
}
