// Input: PostgreSQL DSN 连接字符串
// Output: DB struct（内嵌 *gorm.DB），提供 Connect 和 Close 方法
// Role: GORM PostgreSQL 连接管理，以 Info 级别日志模式建立数据库连接
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package db

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// DB wraps the GORM database connection.
type DB struct {
	*gorm.DB
}

// Connect establishes a connection to PostgreSQL.
func Connect(dsn string) (*DB, error) {
	// 默认 Warn 级别，设置 DB_LOG=true 开启 SQL 日志
	logLevel := logger.Warn
	if os.Getenv("DB_LOG") == "true" {
		logLevel = logger.Info
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		// 禁止 GORM 把字段名转成 snake_case，因为数据库用的是 camelCase 列名
		// 例如 SSHKeyID 默认会被转成 ssh_key_id，但实际列名是 sshKeyId
		NamingStrategy: schema.NamingStrategy{NoLowerCase: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &DB{db}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
