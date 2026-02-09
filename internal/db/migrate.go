package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

type Driver interface {
	EnsureVersionTable(ctx context.Context) error
	ListAppliedVersions(ctx context.Context) ([]int, error)
	ApplyMigration(ctx context.Context, migration Migration) error
	RevertMigration(ctx context.Context, migration Migration) error
}

type Migrator struct {
	driver     Driver
	migrations []Migration
}

func NewMigrator(driver Driver) (*Migrator, error) {
	if driver == nil {
		return nil, fmt.Errorf("driver is required")
	}

	migrations, err := LoadMigrations()
	if err != nil {
		return nil, err
	}

	return &Migrator{
		driver:     driver,
		migrations: migrations,
	}, nil
}

func (m *Migrator) Up(ctx context.Context) (int, error) {
	if err := m.driver.EnsureVersionTable(ctx); err != nil {
		return 0, fmt.Errorf("ensure version table: %w", err)
	}

	applied, err := m.driver.ListAppliedVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("list applied versions: %w", err)
	}

	appliedSet := versionSet(applied)
	pending := pendingUp(m.migrations, appliedSet)

	appliedCount := 0
	for _, migration := range pending {
		if err := m.driver.ApplyMigration(ctx, migration); err != nil {
			return appliedCount, fmt.Errorf("apply migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		appliedCount++
	}

	return appliedCount, nil
}

func (m *Migrator) Down(ctx context.Context, steps int) (int, error) {
	if steps <= 0 {
		return 0, fmt.Errorf("steps must be > 0")
	}

	if err := m.driver.EnsureVersionTable(ctx); err != nil {
		return 0, fmt.Errorf("ensure version table: %w", err)
	}

	applied, err := m.driver.ListAppliedVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("list applied versions: %w", err)
	}

	appliedSet := versionSet(applied)
	revertList := pendingDown(m.migrations, appliedSet, steps)

	revertedCount := 0
	for _, migration := range revertList {
		if err := m.driver.RevertMigration(ctx, migration); err != nil {
			return revertedCount, fmt.Errorf("revert migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		revertedCount++
	}

	return revertedCount, nil
}

func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	byVersion := map[int]*Migration{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		version, name, direction, ok := parseMigrationFilename(filename)
		if !ok {
			continue
		}

		raw, err := fs.ReadFile(migrationFS, filepath.ToSlash(filepath.Join("migrations", filename)))
		if err != nil {
			return nil, fmt.Errorf("read migration file %s: %w", filename, err)
		}

		migration, exists := byVersion[version]
		if !exists {
			migration = &Migration{
				Version: version,
				Name:    name,
			}
			byVersion[version] = migration
		}

		switch direction {
		case "up":
			migration.UpSQL = strings.TrimSpace(string(raw))
		case "down":
			migration.DownSQL = strings.TrimSpace(string(raw))
		}
	}

	migrations := make([]Migration, 0, len(byVersion))
	for _, migration := range byVersion {
		if migration.UpSQL == "" || migration.DownSQL == "" {
			return nil, fmt.Errorf("migration %d_%s must include both up and down sql", migration.Version, migration.Name)
		}
		migrations = append(migrations, *migration)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func parseMigrationFilename(filename string) (int, string, string, bool) {
	parts := strings.Split(filename, ".")
	if len(parts) != 3 {
		return 0, "", "", false
	}
	if parts[2] != "sql" {
		return 0, "", "", false
	}
	if parts[1] != "up" && parts[1] != "down" {
		return 0, "", "", false
	}

	left := parts[0]
	versionAndName := strings.SplitN(left, "_", 2)
	if len(versionAndName) != 2 {
		return 0, "", "", false
	}

	version, err := strconv.Atoi(versionAndName[0])
	if err != nil {
		return 0, "", "", false
	}
	name := versionAndName[1]
	if name == "" {
		return 0, "", "", false
	}

	return version, name, parts[1], true
}

func versionSet(versions []int) map[int]struct{} {
	set := make(map[int]struct{}, len(versions))
	for _, version := range versions {
		set[version] = struct{}{}
	}
	return set
}

func pendingUp(all []Migration, applied map[int]struct{}) []Migration {
	out := make([]Migration, 0)
	for _, migration := range all {
		if _, ok := applied[migration.Version]; ok {
			continue
		}
		out = append(out, migration)
	}
	return out
}

func pendingDown(all []Migration, applied map[int]struct{}, steps int) []Migration {
	out := make([]Migration, 0, steps)
	for i := len(all) - 1; i >= 0; i-- {
		migration := all[i]
		if _, ok := applied[migration.Version]; !ok {
			continue
		}
		out = append(out, migration)
		if len(out) == steps {
			break
		}
	}
	return out
}
