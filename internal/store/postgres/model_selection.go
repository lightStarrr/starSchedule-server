package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/starschedule/server/internal/model"
	"github.com/starschedule/server/internal/store"
)

// 模型新增、同步、别名修改和供应商切换共用事务锁，覆盖尚不存在的行。
func (s *Store) beginModelWrite(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(600451337279)`); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, err
	}
	return tx, nil
}

func modelWriteError(err error) error {
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

// 同一模型继承现有 alias；不同模型不能占用同一个 alias（包括停用记录）。
func modelAlias(ctx context.Context, tx pgx.Tx, modelID, requested string) (string, error) {
	alias := strings.TrimSpace(requested)
	if alias == "" {
		alias = modelID
	}
	var existing string
	err := tx.QueryRow(ctx, `SELECT alias FROM ai_provider_models WHERE model_id=$1 ORDER BY enabled DESC, created_at, id LIMIT 1`, modelID).Scan(&existing)
	if err == nil {
		alias = existing
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var conflict bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_provider_models WHERE alias=$1 AND model_id<>$2)`, alias, modelID).Scan(&conflict); err != nil {
		return "", err
	}
	if conflict {
		return "", store.ErrConflict
	}
	return alias, nil
}

// SetModelAvailability 原子地关闭原供应商、启用目标供应商并迁移默认模型指向。
func (s *Store) SetModelAvailability(ctx context.Context, modelID string, enabled bool, providerID *uuid.UUID) (*model.ProviderModel, error) {
	tx, err := s.beginModelWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, `SELECT `+modelColumns+` FROM ai_provider_models WHERE model_id=$1 ORDER BY enabled DESC, created_at, id FOR UPDATE`, modelID)
	if err != nil {
		return nil, err
	}
	var items []*model.ProviderModel
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, store.ErrNotFound
	}
	target := items[0]
	if providerID != nil {
		target = nil
		for _, m := range items {
			if m.ProviderID == *providerID {
				target = m
				break
			}
		}
		if target == nil {
			return nil, store.ErrNotFound
		}
	}
	if enabled {
		var providerEnabled bool
		if err := tx.QueryRow(ctx, `SELECT enabled FROM ai_providers WHERE id=$1 FOR SHARE`, target.ProviderID).Scan(&providerEnabled); err != nil {
			return nil, err
		}
		if !providerEnabled {
			return nil, store.ErrConflict
		}
	}
	// 启用前再次确认 alias 没有被其他组占用，保护导入的历史配置。
	if enabled {
		if _, err := modelAlias(ctx, tx, modelID, target.Alias); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_provider_models SET enabled=false WHERE model_id=$1 AND enabled`, modelID); err != nil {
		return nil, err
	}
	if enabled {
		if _, err := tx.Exec(ctx, `UPDATE ai_provider_models SET enabled=true WHERE id=$1`, target.ID); err != nil {
			return nil, modelWriteError(err)
		}
		// 默认模型原本属于这一组时，默认设置和供应商选择同时提交。
		var isDefault bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_settings a JOIN ai_provider_models m ON a.value=to_jsonb(m.id::text) WHERE a.key=$1 AND m.model_id=$2)`, keyActiveModel, modelID).Scan(&isDefault); err != nil {
			return nil, err
		}
		if isDefault {
			for _, setting := range []struct{ key, value string }{{keyActiveModel, target.ID.String()}, {keyActiveProvider, target.ProviderID.String()}} {
				if _, err := tx.Exec(ctx, `INSERT INTO app_settings (key,value) VALUES ($1,to_jsonb($2::text)) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, setting.key, setting.value); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	target.Enabled = enabled
	return target, nil
}
