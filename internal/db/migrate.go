// Input: GORM DB 实例, Member 表
// Output: IsAdminPresent bool 方法
// Role: 检查数据库中是否存在 owner 角色成员，用于判断是否需要初始化管理员
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package db

import "github.com/dokploy/dokploy/internal/db/schema"

// IsAdminPresent checks if any admin/owner member exists in the database.
func (d *DB) IsAdminPresent() bool {
	var count int64
	d.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&count)
	return count > 0
}
