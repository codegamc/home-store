package config

import (
	"flag"
	"os"
	"testing"
)

func TestLoadLocationDefaultsToUSEast1(t *testing.T) {
	t.Setenv("HOME_STORE_DATA_DIR", t.TempDir())

	cfg := loadConfigForTest(t)

	if cfg.Location != "us-east-1" {
		t.Fatalf("expected default location us-east-1, got %q", cfg.Location)
	}
}

func TestLoadLocationFromEnvironment(t *testing.T) {
	t.Setenv("HOME_STORE_DATA_DIR", t.TempDir())
	t.Setenv("HOME_STORE_LOCATION", "us-west-2")

	cfg := loadConfigForTest(t)

	if cfg.Location != "us-west-2" {
		t.Fatalf("expected location us-west-2, got %q", cfg.Location)
	}
}

func TestLoadDBPathDefaultsNextToDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME_STORE_DATA_DIR", dataDir)

	cfg := loadConfigForTest(t)

	want := defaultDBPath(dataDir)
	if cfg.DBPath != want {
		t.Fatalf("expected db path %q, got %q", want, cfg.DBPath)
	}
}

func TestLoadDBPathFromEnvironment(t *testing.T) {
	t.Setenv("HOME_STORE_DATA_DIR", t.TempDir())
	t.Setenv("HOME_STORE_DB_PATH", "/tmp/custom-home-store.sqlite")

	cfg := loadConfigForTest(t)

	if cfg.DBPath != "/tmp/custom-home-store.sqlite" {
		t.Fatalf("expected db path from env, got %q", cfg.DBPath)
	}
}

func TestValidateRequiresCredentialsByDefault(t *testing.T) {
	cfg := &Config{Addr: "127.0.0.1:9000", DataDir: t.TempDir(), DBPath: t.TempDir() + "/metadata.sqlite", LogLevel: "info"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to require credentials")
	}
}

func TestValidateAllowsExplicitAuthDisabled(t *testing.T) {
	cfg := &Config{Addr: "127.0.0.1:9000", DataDir: t.TempDir(), DBPath: t.TempDir() + "/metadata.sqlite", LogLevel: "info", AuthDisabled: true}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected auth-disabled config to validate: %v", err)
	}
}

func loadConfigForTest(t *testing.T) *Config {
	t.Helper()

	originalArgs := os.Args
	originalCommandLine := flag.CommandLine

	t.Cleanup(func() {
		os.Args = originalArgs
		flag.CommandLine = originalCommandLine
	})

	os.Args = []string{"home-store"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	return cfg
}
