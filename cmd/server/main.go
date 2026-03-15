package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/soulteary/gorge-file-storage/internal/config"
	"github.com/soulteary/gorge-file-storage/internal/engine"
	"github.com/soulteary/gorge-file-storage/internal/httpapi"

	_ "github.com/go-sql-driver/mysql"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	cfg := config.LoadFromEnv()

	var engines []engine.StorageEngine

	if cfg.MySQLBlobEnabled() {
		db, err := openDB(cfg.FileDSN())
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to connect to MySQL (file db): %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = db.Close() }()
		engines = append(engines, engine.NewMySQLBlobEngine(db, cfg.MySQLMaxSize))
		fmt.Println("engine: mysql-blob enabled (max size:", cfg.MySQLMaxSize, ")")
	}

	if cfg.LocalDiskEnabled() {
		ld, err := engine.NewLocalDiskEngine(cfg.LocalDiskPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init local disk engine: %v\n", err)
			os.Exit(1)
		}
		engines = append(engines, ld)
		fmt.Println("engine: local-disk enabled (path:", cfg.LocalDiskPath, ")")
	}

	if cfg.S3Enabled() {
		s3eng, err := engine.NewS3Engine(engine.S3Config{
			Bucket:       cfg.S3Bucket,
			AccessKey:    cfg.S3AccessKey,
			SecretKey:    cfg.S3SecretKey,
			Region:       cfg.S3Region,
			Endpoint:     cfg.S3Endpoint,
			InstanceName: cfg.InstanceName,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init S3 engine: %v\n", err)
			os.Exit(1)
		}
		engines = append(engines, s3eng)
		fmt.Println("engine: amazon-s3 enabled (bucket:", cfg.S3Bucket, ")")
	}

	if len(engines) == 0 {
		fmt.Fprintln(os.Stderr, "no storage engines configured; set LOCAL_DISK_PATH, MYSQL_BLOB_MAX_SIZE, or S3_* env vars")
		os.Exit(1)
	}

	router := engine.NewRouter(engines)

	e := echo.New()
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true, LogURI: true, LogMethod: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			c.Logger().Infof("%s %s %d", v.Method, v.URI, v.Status)
			return nil
		},
	}))
	e.Use(middleware.Recover())

	httpapi.RegisterRoutes(e, &httpapi.Deps{
		Router: router,
		Token:  cfg.ServiceToken,
	})

	e.Logger.Fatal(e.Start(cfg.ListenAddr))
}

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}
