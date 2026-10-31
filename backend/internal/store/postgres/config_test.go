package postgres

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromProjectRoot(t *testing.T) {
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	if err := os.Mkdir(backend, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_USER=env_test_user\nDB_PORT=5433\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_USER", "old_user")
	t.Setenv("DB_PORT", "5432")
	t.Chdir(backend)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.DBUser != "env_test_user" || config.DBPort != "5433" {
		t.Fatalf("unexpected config: %+v", config)
	}
}
