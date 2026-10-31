package postgres

import "time"

// dateStr 统一把日期转成 YYYY-MM-DD 传给 date 类型参数。
// 直接传 time.Time 会按会话时区做 timestamptz → date 转换，
// 在非 UTC 时区下会把本地零点算成前一天。
func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}
