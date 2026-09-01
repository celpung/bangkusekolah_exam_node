package provider

import (
	"fmt"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	mysqlDriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens the node's database with an explicitly bounded pool. Leaving
// the pool at GORM's default against MySQL's default max_connections of 151 is
// the single most likely way this box fails on exam day.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	dsn, err := normalizeDBDSN(cfg.DBDSN)
	if err != nil {
		return nil, fmt.Errorf("normalize database DSN: %w", err)
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	return db, nil
}

// normalizeDBDSN makes the timezone contract explicit at the only database
// boundary in the node. Exam schedules are absolute instants in the Central ↔
// Node bundle, while MySQL DATETIME itself carries no timezone. Reading or
// writing that column with a deployment-local location can turn the same wall
// clock value into a different instant after a reload.
func normalizeDBDSN(raw string) (string, error) {
	cfg, err := mysqlDriverParseDSN(raw)
	if err != nil {
		return "", err
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	return cfg.FormatDSN(), nil
}

// Kept as a small variable so the DSN policy remains unit-testable without
// opening a database connection.
var mysqlDriverParseDSN = mysqlDriver.ParseDSN
