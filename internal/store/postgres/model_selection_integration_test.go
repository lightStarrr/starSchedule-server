package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/starschedule/server/internal/store"
)

func TestModelSelectionIntegration(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	p1, err := s.CreateProvider(ctx, ProviderInput{Name: "one", BaseURL: "https://example.invalid", APIKeyCipher: []byte{1}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProvider(ctx, ProviderInput{Name: "two", BaseURL: "https://example.invalid", APIKeyCipher: []byte{1}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateModel(ctx, ModelInput{ProviderID: p1.ID, ModelID: "upstream-a", Alias: "stable", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateModel(ctx, ModelInput{ProviderID: p2.ID, ModelID: "upstream-a", Alias: "different", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Enabled || b.Alias != "stable" {
		t.Fatalf("同组应共用 alias 且只启用一家: %+v", b)
	}
	if _, err := s.CreateModel(ctx, ModelInput{ProviderID: p2.ID, ModelID: "other", Alias: "stable", Enabled: false}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("停用记录也不应占用其他组的 alias: %v", err)
	}
	other, err := s.CreateModel(ctx, ModelInput{ProviderID: p2.ID, ModelID: "other", Alias: "other", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	alias := "stable"
	if _, err := s.UpdateModel(ctx, other.ID, ModelPatch{Alias: &alias}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("修改 alias 应拒绝冲突: %v", err)
	}
	yes := true
	if _, err := s.UpdateModel(ctx, b.ID, ModelPatch{Enabled: &yes}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("直接启用不能绕过唯一性: %v", err)
	}
	if err := s.SetActive(ctx, p1.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	selected, err := s.SetModelAvailability(ctx, a.ModelID, true, &p2.ID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.GetActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != b.ID || selected.Alias != "stable" || active.Model.ID != b.ID {
		t.Fatalf("供应商切换应保持别名并迁移默认: %+v %+v", selected, active)
	}

	// 注入启用失败，证明前面的关闭操作也会回滚。
	_, err = s.pool.Exec(ctx, `CREATE FUNCTION reject_enable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.enabled AND NOT OLD.enabled THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_enable BEFORE UPDATE ON ai_provider_models FOR EACH ROW EXECUTE FUNCTION reject_enable()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetModelAvailability(ctx, a.ModelID, true, &p1.ID); err == nil {
		t.Fatal("应注入失败")
	}
	after, err := s.GetModel(ctx, b.ID)
	if err != nil || !after.Enabled {
		t.Fatalf("切换失败应保留原供应商: %+v %v", after, err)
	}
	if _, err = s.pool.Exec(ctx, `DROP TRIGGER reject_enable ON ai_provider_models; DROP FUNCTION reject_enable()`); err != nil {
		t.Fatal(err)
	}

	// 并发切换不能出现两家同时启用或全部关闭。
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := p1.ID
			if i%2 == 0 {
				id = p2.ID
			}
			if _, err := s.SetModelAvailability(ctx, a.ModelID, true, &id); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	rows, err := s.ListModelsByModelID(ctx, a.ModelID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range rows {
		if r.Enabled {
			count++
		}
		if r.Alias != "stable" {
			t.Fatal("切换修改了 alias")
		}
	}
	if count != 1 {
		t.Fatalf("应始终只有一家供应商启用: %d", count)
	}
	// 上游同步也必须继承组别名，冲突时整笔同步回滚。
	if _, err := s.UpsertFetchedModels(ctx, p2.ID, []FetchedModel{{ModelID: "upstream-a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertFetchedModels(ctx, p2.ID, []FetchedModel{{ModelID: "should-rollback"}, {ModelID: "stable"}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("同步应拒绝 alias 冲突: %v", err)
	}
	if rows, err := s.ListModelsByModelID(ctx, "should-rollback"); err != nil || len(rows) != 0 {
		t.Fatalf("同步失败不应部分写入: %v %v", rows, err)
	}
	renamed := "renamed"
	if _, err := s.UpdateModel(ctx, a.ID, ModelPatch{Alias: &renamed}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetModel(ctx, b.ID)
	if err != nil || got.Alias != renamed {
		t.Fatalf("改别名需覆盖整个模型组: %+v %v", got, err)
	}

	// 并发新增同一个上游模型，只能有一条启用记录。
	var create sync.WaitGroup
	for _, pid := range []uuid.UUID{p1.ID, p2.ID} {
		create.Add(1)
		go func(pid uuid.UUID) {
			defer create.Done()
			if _, err := s.CreateModel(ctx, ModelInput{ProviderID: pid, ModelID: "concurrent", Alias: "concurrent", Enabled: true}); err != nil {
				t.Error(err)
			}
		}(pid)
	}
	create.Wait()
	rows, err = s.ListModelsByModelID(ctx, "concurrent")
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, r := range rows {
		if r.Enabled {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("并发新增只允许一家启用: %d", count)
	}
}
