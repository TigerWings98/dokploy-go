// Input: GORM DB 实例, 文件系统中的 Drizzle migration SQL 文件
// Output: IsAdminPresent 检查 + AutoMigrate 幂等迁移（与 TS 版 Drizzle migrate() 行为一致）
// Role: 数据库迁移管理，直接执行 Drizzle 原始 SQL，每次启动幂等执行，确保 Go↔TS 镜像可自由切换
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package db

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dokploy/dokploy/internal/db/schema"
)

// drizzleJournal 对应 Drizzle 的 _journal.json 格式
type drizzleJournal struct {
	Version string               `json:"version"`
	Dialect string               `json:"dialect"`
	Entries []drizzleJournalEntry `json:"entries"`
}

type drizzleJournalEntry struct {
	Idx         int    `json:"idx"`
	Version     string `json:"version"`
	When        int64  `json:"when"`
	Tag         string `json:"tag"`
	Breakpoints bool   `json:"breakpoints"`
}

// IsAdminPresent checks if any admin/owner member exists in the database.
func (d *DB) IsAdminPresent() bool {
	var count int64
	d.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&count)
	return count > 0
}

// AutoMigrate 执行 Drizzle 原始 migration SQL，行为与 TS 版 drizzle-orm/migrate() 完全一致。
// 每次启动都调用，幂等执行：
//   - 读取 drizzle/__drizzle_migrations 记录表，获取最后执行的 migration 时间戳
//   - 只执行 created_at > lastMigrationTimestamp 的新 migration
//   - 全新数据库时执行所有 migration
//   - 已全部执行过则直接跳过
//
// migration SQL 文件从 migrationsDir 目录读取（默认 ./drizzle），镜像中直接 COPY 即可。
func (d *DB) AutoMigrate() error {
	return d.AutoMigrateFrom("./drizzle")
}

// AutoMigrateFrom 从指定目录执行 Drizzle migration。
func (d *DB) AutoMigrateFrom(migrationsDir string) error {
	// 读取 journal
	journalPath := filepath.Join(migrationsDir, "meta", "_journal.json")
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		return fmt.Errorf("failed to read migration journal %s: %w", journalPath, err)
	}

	var journal drizzleJournal
	if err := json.Unmarshal(journalBytes, &journal); err != nil {
		return fmt.Errorf("failed to parse migration journal: %w", err)
	}

	// 按 idx 排序确保顺序
	sort.Slice(journal.Entries, func(i, j int) bool {
		return journal.Entries[i].Idx < journal.Entries[j].Idx
	})

	// 创建 drizzle schema 和 __drizzle_migrations 记录表（幂等）
	if err := d.DB.Exec("CREATE SCHEMA IF NOT EXISTS drizzle").Error; err != nil {
		return fmt.Errorf("failed to create drizzle schema: %w", err)
	}
	if err := d.DB.Exec(`CREATE TABLE IF NOT EXISTS drizzle."__drizzle_migrations" (
		id SERIAL PRIMARY KEY,
		hash text NOT NULL,
		created_at bigint
	)`).Error; err != nil {
		return fmt.Errorf("failed to create drizzle migrations table: %w", err)
	}

	// 查询最后执行的 migration 时间戳（与 Drizzle 一致的逻辑）
	var lastMigration struct {
		CreatedAt *int64
	}
	d.DB.Raw(`SELECT created_at FROM drizzle."__drizzle_migrations" ORDER BY created_at DESC LIMIT 1`).Scan(&lastMigration)

	var lastTimestamp int64
	if lastMigration.CreatedAt != nil {
		lastTimestamp = *lastMigration.CreatedAt
	}

	// 过滤出需要执行的 migration
	var pending []drizzleJournalEntry
	for _, entry := range journal.Entries {
		if entry.When > lastTimestamp {
			pending = append(pending, entry)
		}
	}

	if len(pending) == 0 {
		log.Println("All migrations already applied, nothing to do")
		return nil
	}

	log.Printf("Found %d pending migration(s) to apply (total: %d)", len(pending), len(journal.Entries))

	// 在事务中执行所有 pending migration
	tx := d.DB.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	for _, entry := range pending {
		// 从文件系统读取 SQL
		sqlPath := filepath.Join(migrationsDir, entry.Tag+".sql")
		sqlBytes, err := os.ReadFile(sqlPath)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to read migration file %s: %w", sqlPath, err)
		}

		// 计算 hash（与 Drizzle 一致：SHA256 of 整个文件内容）
		hash := fmt.Sprintf("%x", sha256.Sum256(sqlBytes))

		// 按 --> statement-breakpoint 分割并逐条执行
		statements := strings.Split(string(sqlBytes), "--> statement-breakpoint")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if err := tx.Exec(stmt).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s failed: %w\nSQL: %s", entry.Tag, err, truncateSQL(stmt))
			}
		}

		// 记录到 drizzle.__drizzle_migrations（与 Drizzle 写入逻辑一致）
		if err := tx.Exec(
			`INSERT INTO drizzle."__drizzle_migrations" ("hash", "created_at") VALUES (?, ?)`,
			hash, entry.When,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", entry.Tag, err)
		}

		log.Printf("Applied migration: %s", entry.Tag)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}

	log.Printf("Successfully applied %d migration(s)", len(pending))
	return nil
}

// truncateSQL 截断 SQL 用于错误日志，避免日志过长
func truncateSQL(sql string) string {
	if len(sql) > 200 {
		return sql[:200] + "..."
	}
	return sql
}
