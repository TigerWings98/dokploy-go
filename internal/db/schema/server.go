// Input: gorm, go-nanoid, database/sql/driver, encoding/json
// Output: Server struct (含 ipAddress/port/sshKeyId/serverStatus 等字段) + ServerDeployment struct
// Role: 远程服务器和服务器部署记录数据表模型，关联 SSHKey/Organization，支持自定义 JSON 序列化
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Server represents the server table.
type Server struct {
	ServerID            string       `gorm:"column:serverId;primaryKey;type:text" json:"serverId"`
	Name                string       `gorm:"column:name;type:text;not null" json:"name"`
	Description         *string      `gorm:"column:description;type:text" json:"description"`
	IPAddress           string       `gorm:"column:ipAddress;type:text;not null" json:"ipAddress"`
	Port                int          `gorm:"column:port;not null" json:"port"`
	Username            string       `gorm:"column:username;type:text;not null;default:'root'" json:"username"`
	AppName             string       `gorm:"column:appName;type:text;not null" json:"appName"`
	EnableDockerCleanup bool         `gorm:"column:enableDockerCleanup;not null;default:false" json:"enableDockerCleanup"`
	CreatedAt           string       `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	OrganizationID      string       `gorm:"column:organizationId;type:text;not null" json:"organizationId"`
	ServerStatus        ServerStatus `gorm:"column:serverStatus;type:text;not null;default:'active'" json:"serverStatus"`
	ServerType          ServerType   `gorm:"column:serverType;type:text;not null;default:'deploy'" json:"serverType"`
	Command             string       `gorm:"column:command;type:text;not null;default:''" json:"command"`
	SSHKeyID            *string      `gorm:"column:sshKeyId;type:text" json:"sshKeyId"`
	MetricsConfig       *MetricsConfigJSON `gorm:"column:metricsConfig;type:jsonb" json:"metricsConfig"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
	SSHKey       *SSHKey       `gorm:"foreignKey:SSHKeyID;references:SSHKeyID" json:"sshKey,omitempty"`
	Applications []Application `gorm:"foreignKey:ServerID;references:ServerID" json:"applications"`
	Deployments  []Deployment  `gorm:"foreignKey:ServerID;references:ServerID" json:"deployments"`
}

func (Server) TableName() string { return "server" }

func (s *Server) BeforeCreate(tx *gorm.DB) error {
	if s.ServerID == "" {
		s.ServerID, _ = gonanoid.New()
	}
	if s.AppName == "" {
		s.AppName = GenerateAppName("server")
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// MetricsConfig represents the server metrics configuration.
type MetricsConfig struct {
	Server     MetricsServerConfig     `json:"server"`
	Containers MetricsContainersConfig `json:"containers"`
}

type MetricsServerConfig struct {
	Type          string                   `json:"type"`
	RefreshRate   int                      `json:"refreshRate"`
	Port          int                      `json:"port"`
	Token         string                   `json:"token"`
	URLCallback   string                   `json:"urlCallback"`
	RetentionDays int                      `json:"retentionDays"`
	CronJob       string                   `json:"cronJob"`
	Thresholds    MetricsThresholdsConfig  `json:"thresholds"`
}

type MetricsThresholdsConfig struct {
	CPU    int `json:"cpu"`
	Memory int `json:"memory"`
}

type MetricsContainersConfig struct {
	RefreshRate int                          `json:"refreshRate"`
	Services    MetricsServicesFilterConfig  `json:"services"`
}

type MetricsServicesFilterConfig struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// MetricsConfigJSON implements driver.Valuer and sql.Scanner for GORM JSONB.
type MetricsConfigJSON MetricsConfig

func (m MetricsConfigJSON) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *MetricsConfigJSON) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return nil
	}
	return json.Unmarshal(bytes, m)
}
