package redis

import (
	"context"
	"encoding/json"
	"time"
)

// GetJSON 读取 JSON 缓存，未命中返回 false。
func (c *Client) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	if err := c.gate(); err != nil {
		return false, err
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err.Error() == "redis: nil" {
			return false, nil
		}
		c.record(err)
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	if err := c.gate(); err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	err = c.rdb.Set(ctx, key, raw, ttl).Err()
	c.record(err)
	return err
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := c.gate(); err != nil {
		return err
	}
	err := c.rdb.Del(ctx, keys...).Err()
	c.record(err)
	return err
}

// Cache keys（集中定义，避免拼写漂移）。
const (
	KeyAIActive  = "cache:ai:active:v2"
	KeyAICatalog = "cache:ai:catalog:v2"
	KeySettings  = "cache:settings:v2"
)

func BanKey(deviceID string) string { return "ban:device:" + deviceID }
