package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/starschedule/server/internal/store"
)

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
