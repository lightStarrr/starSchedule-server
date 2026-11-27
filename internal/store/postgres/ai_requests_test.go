package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func intPtr(v int) *int { return &v }

func TestComputeStreamTPS(t *testing.T) {
	cases := []struct {
		name       string
		stream     bool
		completion int
		first      *int
		total      *int
		wantNil    bool
		want       float64
	}{
		{
			name:       "流式扣除首Token计算纯生成速度",
			stream:     true,
			completion: 240,
			first:      intPtr(500),
			total:      intPtr(3500),
			want:       80.0,
		},
		{
			name:       "保留一位小数",
			stream:     true,
			completion: 100,
			first:      intPtr(300),
			total:      intPtr(1800),
			want:       66.7,
		},
		{
			name:       "非流式不计算TPS",
			stream:     false,
			completion: 240,
			first:      nil,
			total:      intPtr(2000),
			wantNil:    true,
		},
		{
			name:       "缺少首Token耗时不混算总耗时",
			stream:     true,
			completion: 240,
			first:      nil,
			total:      intPtr(2000),
			wantNil:    true,
		},
		{
			name:       "纯生成耗时为0不计算",
			stream:     true,
			completion: 20,
			first:      intPtr(500),
			total:      intPtr(500),
			wantNil:    true,
		},
		{
			name:       "零输出不计算",
			stream:     true,
			completion: 0,
			first:      intPtr(200),
			total:      intPtr(1000),
			wantNil:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeStreamTPS(tc.stream, tc.completion, tc.first, tc.total)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("期望 nil，实际 %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("期望返回 TPS，实际为 nil")
			}
			if *got != tc.want {
				t.Fatalf("TPS = %v，期望 %v", *got, tc.want)
			}
		})
	}
}

func TestAIRequestCursorRoundTrip(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC().Truncate(time.Microsecond)
	encoded := encodeAIRequestCursor(aiRequestCursor{CreatedAt: now, ID: id})
	decoded, err := decodeAIRequestCursor(encoded)
	if err != nil {
		t.Fatalf("解码游标失败: %v", err)
	}
	if !decoded.CreatedAt.Equal(now) || decoded.ID != id {
		t.Fatalf("游标往返不一致: %+v vs (%v, %v)", decoded, now, id)
	}
	if _, err := decodeAIRequestCursor("not-valid-base64!"); err == nil {
		t.Fatal("非法游标应报错")
	}
}
