package db

import (
	"context"
	"database/sql"
	"fmt"
)

type SQLDriver struct {
	db *sql.DB
}

func NewSQLDriver(db *sql.DB) *SQLDriver {
	return &SQLDriver{db: db}
}

func (d *SQLDriver) EnsureVersionTable(ctx context.Context) error {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version BIGINT PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`
	_, err := d.db.ExecContext(ctx, query)
	return err
}

func (d *SQLDriver) ListAppliedVersions(ctx context.Context) ([]int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []int{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return versions, nil
}

func (d *SQLDriver) ApplyMigration(ctx context.Context, migration Migration) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, migration.UpSQL); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2);`,
		migration.Version,
		migration.Name,
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (d *SQLDriver) RevertMigration(ctx context.Context, migration Migration) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, migration.DownSQL); err != nil {
		_ = tx.Rollback()
		return err
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1;`, migration.Version)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if rowsAffected == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("version %d not marked as applied", migration.Version)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
