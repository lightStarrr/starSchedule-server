package redis

import (
	"context"
	"time"
)

const banTTL = time.Hour

// SetBanned 把封禁状态写进 Redis，避免每次请求都打 DB。
func (c *Client) SetBanned(ctx context.Context, deviceID string) {
	if err := c.gate(); err != nil {
		return
	}
	err := c.rdb.Set(ctx, BanKey(deviceID), "1", banTTL).Err()
	c.record(err)
}

func (c *Client) ClearBanned(ctx context.Context, deviceID string) {
	_ = c.Del(ctx, BanKey(deviceID))
}

// IsBanned 返回 (是否封禁, 是否命中缓存)。
func (c *Client) IsBanned(ctx context.Context, deviceID string) (bool, bool) {
	if err := c.gate(); err != nil {
		return false, false
	}
	n, err := c.rdb.Exists(ctx, BanKey(deviceID)).Result()
	if err != nil {
		c.record(err)
		return false, false
	}
	return n > 0, true
}
