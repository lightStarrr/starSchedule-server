package redis

import (
	"context"
	"time"
)

// 预扣：把「提示词估算 + max_tokens」先占住，防并发绕过额度。
var quotaPrechargeScript = `
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
local amount = tonumber(ARGV[1])
local quota = tonumber(ARGV[2])
if used + amount > quota then
  return {0, used}
end
local nv = redis.call('INCRBY', KEYS[1], amount)
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[3])
end
return {1, nv}
`

// 结算：delta 为负表示退还多扣的额度。
var quotaSettleScript = `
local key = KEYS[1]
local delta = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local cur = tonumber(redis.call('GET', key) or '0')
local nv = cur
if delta < 0 then
  local refund = -delta
  if refund > cur then refund = cur end
  if refund > 0 then nv = redis.call('DECRBY', key, refund) end
elseif delta > 0 then
  nv = redis.call('INCRBY', key, delta)
end
if redis.call('TTL', key) < 0 then
  redis.call('EXPIRE', key, ttl)
end
return nv
`

func QuotaKey(deviceID, date string) string { return "quota:ai:" + date + ":" + deviceID }

// PrechargeQuota 返回 (是否通过, 当前已用量)。Redis 异常时由调用方 fail-closed。
func (c *Client) PrechargeQuota(ctx context.Context, key string, amount, quota int64, ttl time.Duration) (bool, int64, error) {
	if err := c.gate(); err != nil {
		return false, 0, err
	}
	secs := int(ttl.Seconds())
	if secs <= 0 {
		secs = 86400
	}
	res, err := c.rdb.Eval(ctx, quotaPrechargeScript, []string{key}, amount, quota, secs).Slice()
	c.record(err)
	if err != nil {
		return false, 0, err
	}
	allowed, _ := res[0].(int64)
	used, _ := res[1].(int64)
	return allowed == 1, used, nil
}

func (c *Client) SettleQuota(ctx context.Context, key string, delta int64, ttl time.Duration) error {
	if delta == 0 {
		return nil
	}
	if err := c.gate(); err != nil {
		return err
	}
	secs := int(ttl.Seconds())
	if secs <= 0 {
		secs = 86400
	}
	err := c.rdb.Eval(ctx, quotaSettleScript, []string{key}, delta, secs).Err()
	c.record(err)
	return err
}

func (c *Client) GetQuotaUsed(ctx context.Context, key string) (int64, error) {
	if err := c.gate(); err != nil {
		return 0, err
	}
	n, err := c.rdb.Get(ctx, key).Int64()
	if err != nil {
		if err.Error() == "redis: nil" {
			return 0, nil
		}
		c.record(err)
		return 0, err
	}
	return n, nil
}
