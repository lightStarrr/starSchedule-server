package postgres

import (
	"encoding/json"
	"testing"
)

func TestRemainingTokens(t *testing.T) {
	for _, tc := range []struct {
		name      string
		unlimited bool
		used      int64
		want      string
	}{{"有限额", false, 30, "70"}, {"已用完", false, 130, "0"}, {"不限量", true, 130, "null"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := RuntimeSettings{DailyTokenQuota: 100, QuotaUnlimited: tc.unlimited}
			raw, err := json.Marshal(s.RemainingTokens(tc.used))
			if err != nil || string(raw) != tc.want {
				t.Fatalf("剩余额度: %s %v", raw, err)
			}
		})
	}
}
