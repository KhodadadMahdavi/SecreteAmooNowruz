package db

import (
	"context"
	"fmt"
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	t.Helper()

	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("LoadMigrations() returned no migrations")
	}

	first := migrations[0]
	if first.Version != 1 {
		t.Fatalf("first migration version = %d, want 1", first.Version)
	}
	if first.UpSQL == "" || first.DownSQL == "" {
		t.Fatalf("migration %d is missing SQL content", first.Version)
	}
}

func TestMigratorUpIsIdempotent(t *testing.T) {
	t.Helper()

	driver := newMemoryDriver()
	migrator, err := NewMigrator(driver)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}

	applied, err := migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() first run error = %v", err)
	}
	if applied != 1 {
		t.Fatalf("Up() first run applied = %d, want 1", applied)
	}

	applied, err = migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() second run error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("Up() second run applied = %d, want 0", applied)
	}
}

func TestMigratorDown(t *testing.T) {
	t.Helper()

	driver := newMemoryDriver()
	migrator, err := NewMigrator(driver)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}

	if _, err := migrator.Up(context.Background()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	reverted, err := migrator.Down(context.Background(), 1)
	if err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if reverted != 1 {
		t.Fatalf("Down() reverted = %d, want 1", reverted)
	}

	applied, err := driver.ListAppliedVersions(context.Background())
	if err != nil {
		t.Fatalf("ListAppliedVersions() error = %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied versions count = %d, want 0", len(applied))
	}
}

func TestMigratorDownRequiresPositiveSteps(t *testing.T) {
	t.Helper()

	driver := newMemoryDriver()
	migrator, err := NewMigrator(driver)
	if err != nil {
		t.Fatalf("NewMigrator() error = %v", err)
	}

	_, err = migrator.Down(context.Background(), 0)
	if err == nil {
		t.Fatal("Down() error = nil, want error")
	}
}

type memoryDriver struct {
	applied map[int]Migration
}

func newMemoryDriver() *memoryDriver {
	return &memoryDriver{
		applied: map[int]Migration{},
	}
}

func (d *memoryDriver) EnsureVersionTable(context.Context) error {
	return nil
}

func (d *memoryDriver) ListAppliedVersions(context.Context) ([]int, error) {
	out := make([]int, 0, len(d.applied))
	for version := range d.applied {
		out = append(out, version)
	}

	// manual sort for tiny set to avoid importing additional package just for tests
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	return out, nil
}

func (d *memoryDriver) ApplyMigration(_ context.Context, migration Migration) error {
	if _, exists := d.applied[migration.Version]; exists {
		return fmt.Errorf("version %d already applied", migration.Version)
	}
	d.applied[migration.Version] = migration
	return nil
}

func (d *memoryDriver) RevertMigration(_ context.Context, migration Migration) error {
	if _, exists := d.applied[migration.Version]; !exists {
		return fmt.Errorf("version %d not applied", migration.Version)
	}
	delete(d.applied, migration.Version)
	return nil
}
