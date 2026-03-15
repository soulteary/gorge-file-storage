package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	MySQLHost string
	MySQLPort int
	MySQLUser string
	MySQLPass string
	Namespace string

	ListenAddr   string
	ServiceToken string

	LocalDiskPath string
	MySQLMaxSize  int64 // max blob size in bytes, 0 = disabled
	InstanceName  string
	StorageRoot   string // local disk mount inside container

	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	S3Endpoint  string
}

func LoadFromEnv() *Config {
	return &Config{
		MySQLHost:    envStr("MYSQL_HOST", "127.0.0.1"),
		MySQLPort:    envInt("MYSQL_PORT", 3306),
		MySQLUser:    envStr("MYSQL_USER", "phorge"),
		MySQLPass:    envStr("MYSQL_PASS", ""),
		Namespace:    envStr("STORAGE_NAMESPACE", "phorge"),
		ListenAddr:   envStr("LISTEN_ADDR", ":8100"),
		ServiceToken: envStr("SERVICE_TOKEN", ""),

		LocalDiskPath: envStr("LOCAL_DISK_PATH", ""),
		MySQLMaxSize:  envInt64("MYSQL_BLOB_MAX_SIZE", 1000000),
		InstanceName:  envStr("INSTANCE_NAME", ""),
		StorageRoot:   envStr("STORAGE_ROOT", "/var/lib/phorge/files"),

		S3Bucket:    envStr("S3_BUCKET", ""),
		S3AccessKey: envStr("S3_ACCESS_KEY", ""),
		S3SecretKey: envStr("S3_SECRET_KEY", ""),
		S3Region:    envStr("S3_REGION", ""),
		S3Endpoint:  envStr("S3_ENDPOINT", ""),
	}
}

func (c *Config) FileDSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s_file?parseTime=true&timeout=5s&readTimeout=30s&writeTimeout=30s",
		c.MySQLUser, c.MySQLPass, c.MySQLHost, c.MySQLPort, c.Namespace,
	)
}

func (c *Config) S3Enabled() bool {
	return c.S3Bucket != "" && c.S3AccessKey != "" && c.S3SecretKey != "" &&
		c.S3Region != "" && c.S3Endpoint != ""
}

func (c *Config) LocalDiskEnabled() bool {
	return c.LocalDiskPath != ""
}

func (c *Config) MySQLBlobEnabled() bool {
	return c.MySQLMaxSize > 0
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return n
		}
	}
	return fallback
}
