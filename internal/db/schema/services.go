package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// Port represents the port table.
type Port struct {
	PortID        string       `gorm:"column:portId;primaryKey;type:text" json:"portId"`
	PublishedPort int          `gorm:"column:publishedPort;not null" json:"publishedPort"`
	TargetPort    int          `gorm:"column:targetPort;not null" json:"targetPort"`
	Protocol      ProtocolType `gorm:"column:protocol;type:text;not null;default:'tcp'" json:"protocol"`
	CreatedAt     string       `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID *string      `gorm:"column:applicationId;type:text" json:"applicationId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
}

func (Port) TableName() string { return "port" }

func (p *Port) BeforeCreate(tx *gorm.DB) error {
	if p.PortID == "" {
		p.PortID, _ = gonanoid.New()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Security represents the security table (basic auth).
type Security struct {
	SecurityID    string  `gorm:"column:securityId;primaryKey;type:text" json:"securityId"`
	Username      string  `gorm:"column:username;type:text;not null" json:"username"`
	Password      string  `gorm:"column:password;type:text;not null" json:"password"`
	CreatedAt     string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID     *string `gorm:"column:composeId;type:text" json:"composeId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
}

func (Security) TableName() string { return "security" }

func (s *Security) BeforeCreate(tx *gorm.DB) error {
	if s.SecurityID == "" {
		s.SecurityID, _ = gonanoid.New()
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Redirect represents the redirects table.
type Redirect struct {
	RedirectID    string  `gorm:"column:redirectId;primaryKey;type:text" json:"redirectId"`
	Regex         string  `gorm:"column:regex;type:text;not null" json:"regex"`
	Replacement   string  `gorm:"column:replacement;type:text;not null" json:"replacement"`
	Permanent     bool    `gorm:"column:permanent;not null;default:false" json:"permanent"`
	UniqueConfigKey *int  `gorm:"column:uniqueConfigKey" json:"uniqueConfigKey"`
	CreatedAt     string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ApplicationID *string `gorm:"column:applicationId;type:text" json:"applicationId"`
	ComposeID     *string `gorm:"column:composeId;type:text" json:"composeId"`

	Application *Application `gorm:"foreignKey:ApplicationID" json:"application,omitempty"`
	Compose     *Compose     `gorm:"foreignKey:ComposeID" json:"compose,omitempty"`
}

func (Redirect) TableName() string { return "redirect" }

func (r *Redirect) BeforeCreate(tx *gorm.DB) error {
	if r.RedirectID == "" {
		r.RedirectID, _ = gonanoid.New()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Registry represents the registry table.
type Registry struct {
	RegistryID     string       `gorm:"column:registryId;primaryKey;type:text" json:"registryId"`
	RegistryName   string       `gorm:"column:registryName;type:text;not null" json:"registryName"`
	ImagePrefix    *string      `gorm:"column:imagePrefix;type:text" json:"imagePrefix"`
	Username       string       `gorm:"column:username;type:text;not null" json:"username"`
	Password       string       `gorm:"column:password;type:text;not null" json:"password"`
	RegistryURL    string       `gorm:"column:registryUrl;type:text;not null" json:"registryUrl"`
	RegistryType   RegistryType `gorm:"column:registryType;type:text;not null;default:'cloud'" json:"registryType"`
	CreatedAt      string       `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	SelfHostedImage *string     `gorm:"column:selfHostedImage;type:text" json:"selfHostedImage"`
	OrganizationID string       `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
}

func (Registry) TableName() string { return "registry" }

func (r *Registry) BeforeCreate(tx *gorm.DB) error {
	if r.RegistryID == "" {
		r.RegistryID, _ = gonanoid.New()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// SSHKey represents the ssh-key table.
type SSHKey struct {
	SSHKeyID       string  `gorm:"column:sshKeyId;primaryKey;type:text" json:"sshKeyId"`
	PublicKey      string  `gorm:"column:publicKey;type:text;not null" json:"publicKey"`
	Name           string  `gorm:"column:name;type:text;not null" json:"name"`
	Description    *string `gorm:"column:description;type:text" json:"description"`
	CreatedAt      string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	LastUsedAt     *string `gorm:"column:lastUsedAt;type:text" json:"lastUsedAt"`
	PrivateKey     string  `gorm:"column:privateKey;type:text;not null" json:"privateKey"`
	OrganizationID string  `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
}

func (SSHKey) TableName() string { return "ssh-key" }

func (s *SSHKey) BeforeCreate(tx *gorm.DB) error {
	if s.SSHKeyID == "" {
		s.SSHKeyID, _ = gonanoid.New()
	}
	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Certificate represents the certificate table.
type Certificate struct {
	CertificateID   string  `gorm:"column:certificateId;primaryKey;type:text" json:"certificateId"`
	Name            string  `gorm:"column:name;type:text;not null" json:"name"`
	CertificateData string  `gorm:"column:certificateData;type:text;not null" json:"certificateData"`
	PrivateKey      string  `gorm:"column:privateKey;type:text;not null" json:"privateKey"`
	CertificatePath *string `gorm:"column:certificatePath;type:text" json:"certificatePath"`
	AutoRenew       *bool   `gorm:"column:autoRenew" json:"autoRenew"`
	CreatedAt       string  `gorm:"column:createdAt;type:text;not null" json:"createdAt"`
	ServerID        *string `gorm:"column:serverId;type:text" json:"serverId"`
	OrganizationID  string  `gorm:"column:organizationId;type:text;not null" json:"organizationId"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	Server       *Server       `gorm:"foreignKey:ServerID" json:"server,omitempty"`
}

func (Certificate) TableName() string { return "certificate" }

func (c *Certificate) BeforeCreate(tx *gorm.DB) error {
	if c.CertificateID == "" {
		c.CertificateID, _ = gonanoid.New()
	}
	if c.CreatedAt == "" {
		c.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
