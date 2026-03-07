package db

import "github.com/dokploy/dokploy/internal/db/schema"

// IsAdminPresent checks if any admin/owner member exists in the database.
func (d *DB) IsAdminPresent() bool {
	var count int64
	d.DB.Model(&schema.Member{}).Where("role = ?", "owner").Count(&count)
	return count > 0
}
