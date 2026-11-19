package database

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"time"

	"ppclassvip/internal/config"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func Open(cfg config.MySQLConfig, log *slog.Logger) (*gorm.DB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("mysql dsn is empty")
	}

	gormLog := gormlogger.New(
		stdlog.New(os.Stdout, "", stdlog.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
	db, err := gorm.Open(gormmysql.Open(cfg.DSN), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime.Std())
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime.Std())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	log.Info("mysql connected",
		"max_open_conns", cfg.MaxOpenConns,
		"max_idle_conns", cfg.MaxIdleConns,
		"conn_max_lifetime", cfg.ConnMaxLifetime.Std().String(),
	)
	return db, nil
}

func AutoMigrate(db *gorm.DB, models ...any) error {
	if len(models) == 0 {
		return nil
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
