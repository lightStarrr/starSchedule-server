package redis

import (
	"context"
	"time"
)

var fixedWindowScript = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then ttl = tonumber(ARGV[1]) end
return {n, ttl}
`

var inflightAcquireScript = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
if n > tonumber(ARGV[2]) then
  redis.call('DECR', KEYS[1])
  return 0
end
return 1
`

var inflightReleaseScript = `
local n = tonumber(redis.call('GET', KEYS[1]) or '0')
if n > 0 then
  redis.call('DECR', KEYS[1])
end
return 1
`

// AllowFixedWindow 固定窗口限流。返回是否放行与建议重试秒数。
func (c *Client) AllowFixedWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	if limit <= 0 {
		return true, 0, nil
	}
	if err := c.gate(); err != nil {
		return false, 0, err
	}
	secs := int(window.Seconds())
	if secs <= 0 {
		secs = 1
	}
	res, err := c.rdb.Eval(ctx, fixedWindowScript, []string{key}, secs).Slice()
	c.record(err)
	if err != nil {
		return false, 0, err
	}
	n, _ := res[0].(int64)
	ttl, _ := res[1].(int64)
	return n <= int64(limit), int(ttl), nil
}

// AcquireInflight 并发闸门，lease 到期自动回收，避免进程崩溃后永久占位。
func (c *Client) AcquireInflight(ctx context.Context, key string, limit int, lease time.Duration) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	if err := c.gate(); err != nil {
		return false, err
	}
	n, err := c.rdb.Eval(ctx, inflightAcquireScript, []string{key}, int(lease.Seconds()), limit).Int()
	c.record(err)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (c *Client) ReleaseInflight(ctx context.Context, key string) {
	if err := c.gate(); err != nil {
		return
	}
	err := c.rdb.Eval(ctx, inflightReleaseScript, []string{key}).Err()
	c.record(err)
}
