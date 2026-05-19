package config

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrationsDiscoversSQLFilesInOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"assets/migrations/010_second.sql": {Data: []byte("SELECT 2;")},
		"assets/migrations/001_first.sql":  {Data: []byte("SELECT 1;")},
		"assets/migrations/README.md":      {Data: []byte("ignored")},
	}

	got, err := loadMigrations(fsys)
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(migrations) = %d, want 2", len(got))
	}

	if got[0].Version != "001_first" {
		t.Fatalf("first version = %q, want %q", got[0].Version, "001_first")
	}
	if got[1].Version != "010_second" {
		t.Fatalf("second version = %q, want %q", got[1].Version, "010_second")
	}
}

func TestEmbeddedEventIDMigrationIsPresent(t *testing.T) {
	migrations, err := loadMigrations(assetsFS)
	if err != nil {
		t.Fatalf("loadMigrations(assetsFS) error = %v", err)
	}

	for _, migration := range migrations {
		if migration.Version == "001_k8s_events_event_id" {
			return
		}
	}

	t.Fatal("embedded migration 001_k8s_events_event_id not found")
}
