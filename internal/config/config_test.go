package config

import (
	"os"
	"testing"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	for _, key := range []string{
		"MYSQL_HOST", "MYSQL_PORT", "MYSQL_USER", "MYSQL_PASS",
		"STORAGE_NAMESPACE", "LISTEN_ADDR", "SERVICE_TOKEN",
		"LOCAL_DISK_PATH", "MYSQL_BLOB_MAX_SIZE", "INSTANCE_NAME",
		"S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_REGION", "S3_ENDPOINT",
	} {
		_ = os.Unsetenv(key)
	}

	cfg := LoadFromEnv()

	if cfg.MySQLHost != "127.0.0.1" {
		t.Errorf("MySQLHost = %q, want 127.0.0.1", cfg.MySQLHost)
	}
	if cfg.MySQLPort != 3306 {
		t.Errorf("MySQLPort = %d, want 3306", cfg.MySQLPort)
	}
	if cfg.ListenAddr != ":8100" {
		t.Errorf("ListenAddr = %q, want :8100", cfg.ListenAddr)
	}
	if cfg.MySQLMaxSize != 1000000 {
		t.Errorf("MySQLMaxSize = %d, want 1000000", cfg.MySQLMaxSize)
	}
	if cfg.S3Enabled() {
		t.Error("S3Enabled should be false with defaults")
	}
	if cfg.LocalDiskEnabled() {
		t.Error("LocalDiskEnabled should be false with defaults")
	}
	if !cfg.MySQLBlobEnabled() {
		t.Error("MySQLBlobEnabled should be true with default max size")
	}
}

func TestLoadFromEnvCustom(t *testing.T) {
	t.Setenv("S3_BUCKET", "my-bucket")
	t.Setenv("S3_ACCESS_KEY", "AKID")
	t.Setenv("S3_SECRET_KEY", "secret")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_ENDPOINT", "https://s3.amazonaws.com")
	t.Setenv("LOCAL_DISK_PATH", "/data/files")
	t.Setenv("MYSQL_BLOB_MAX_SIZE", "0")

	cfg := LoadFromEnv()

	if !cfg.S3Enabled() {
		t.Error("S3Enabled should be true")
	}
	if !cfg.LocalDiskEnabled() {
		t.Error("LocalDiskEnabled should be true")
	}
	if cfg.MySQLBlobEnabled() {
		t.Error("MySQLBlobEnabled should be false when max size is 0")
	}
}

func TestFileDSN(t *testing.T) {
	cfg := &Config{
		MySQLUser: "phorge",
		MySQLPass: "secret",
		MySQLHost: "db",
		MySQLPort: 3306,
		Namespace: "phorge",
	}
	dsn := cfg.FileDSN()
	want := "phorge:secret@tcp(db:3306)/phorge_file?parseTime=true&timeout=5s&readTimeout=30s&writeTimeout=30s"
	if dsn != want {
		t.Errorf("FileDSN() = %q, want %q", dsn, want)
	}
}
