// Input: gorm, go-nanoid
// Output: User/Session/Account/Verification/Organization/Member/Invitation/APIKey struct
// Role: Better Auth 兼容的用户认证体系数据表模型，涵盖用户/会话/OAuth账号/组织/成员/API密钥
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"gorm.io/gorm"
)

// User represents the user table.
type User struct {
	ID                       string     `gorm:"column:id;primaryKey;type:text" json:"id"`
	FirstName                string     `gorm:"column:firstName;type:text;not null;default:''" json:"firstName"`
	LastName                 string     `gorm:"column:lastName;type:text;not null;default:''" json:"lastName"`
	IsRegistered             bool       `gorm:"column:isRegistered;not null;default:false" json:"isRegistered"`
	ExpirationDate           string     `gorm:"column:expirationDate;type:text;not null" json:"expirationDate"`
	CreatedAt2               string     `gorm:"column:createdAt;type:text;not null" json:"createdAt2"`
	CreatedAt                *time.Time `gorm:"column:created_at" json:"createdAt"`
	TwoFactorEnabled         *bool      `gorm:"column:two_factor_enabled" json:"twoFactorEnabled"`
	Email                    string     `gorm:"column:email;type:text;not null;uniqueIndex:user_email_unique" json:"email"`
	EmailVerified            bool       `gorm:"column:email_verified;not null" json:"emailVerified"`
	Image                    *string    `gorm:"column:image;type:text" json:"image"`
	Banned                   *bool      `gorm:"column:banned" json:"banned"`
	BanReason                *string    `gorm:"column:ban_reason;type:text" json:"banReason"`
	BanExpires               *time.Time `gorm:"column:ban_expires" json:"banExpires"`
	UpdatedAt                time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
	Role                     string     `gorm:"column:role;type:text;not null;default:'user'" json:"role"`
	EnablePaidFeatures       bool       `gorm:"column:enablePaidFeatures;not null;default:false" json:"enablePaidFeatures"`
	AllowImpersonation       bool       `gorm:"column:allowImpersonation;not null;default:false" json:"allowImpersonation"`
	EnableEnterpriseFeatures bool       `gorm:"column:enableEnterpriseFeatures;not null;default:false" json:"enableEnterpriseFeatures"`
	LicenseKey               *string    `gorm:"column:licenseKey;type:text" json:"licenseKey"`
	IsValidEnterpriseLicense bool       `gorm:"column:isValidEnterpriseLicense;not null;default:false" json:"isValidEnterpriseLicense"`
	StripeCustomerID         *string    `gorm:"column:stripeCustomerId;type:text" json:"stripeCustomerId"`
	StripeSubscriptionID     *string    `gorm:"column:stripeSubscriptionId;type:text" json:"stripeSubscriptionId"`
	ServersQuantity          int        `gorm:"column:serversQuantity;not null;default:0" json:"serversQuantity"`
	TrustedOrigins           StringArray `gorm:"column:trustedOrigins;type:text[]" json:"trustedOrigins"`

	// Relations
	Account       *Account       `gorm:"foreignKey:UserID;references:ID" json:"account,omitempty"`
	Organizations []Organization `gorm:"foreignKey:OwnerID;references:ID" json:"organizations"`
	APIKeys       []APIKey       `gorm:"foreignKey:UserID;references:ID" json:"apiKeys"`
	Backups       []Backup       `gorm:"foreignKey:UserID;references:ID" json:"backups"`
}

func (User) TableName() string { return "user" }

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID, _ = gonanoid.New()
	}
	if u.ExpirationDate == "" {
		u.ExpirationDate = time.Now().UTC().Format(time.RFC3339)
	}
	if u.CreatedAt2 == "" {
		u.CreatedAt2 = time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}

// Account represents the account table (Better Auth).
type Account struct {
	ID                    string     `gorm:"column:id;primaryKey;type:text" json:"id"`
	AccountID             string     `gorm:"column:account_id;type:text;not null" json:"accountId"`
	ProviderID            string     `gorm:"column:provider_id;type:text;not null" json:"providerId"`
	UserID                string     `gorm:"column:user_id;type:text;not null" json:"userId"`
	AccessToken           *string    `gorm:"column:access_token;type:text" json:"accessToken"`
	RefreshToken          *string    `gorm:"column:refresh_token;type:text" json:"refreshToken"`
	IDToken               *string    `gorm:"column:id_token;type:text" json:"idToken"`
	AccessTokenExpiresAt  *time.Time `gorm:"column:access_token_expires_at" json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt *time.Time `gorm:"column:refresh_token_expires_at" json:"refreshTokenExpiresAt"`
	Scope                 *string    `gorm:"column:scope;type:text" json:"scope"`
	Password              *string    `gorm:"column:password;type:text" json:"password"`
	Is2FAEnabled          bool       `gorm:"column:is2FAEnabled;not null;default:false" json:"is2FAEnabled"`
	CreatedAt             time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt             time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
	ResetPasswordToken    *string    `gorm:"column:resetPasswordToken;type:text" json:"resetPasswordToken"`
	ResetPasswordExpiresAt *string   `gorm:"column:resetPasswordExpiresAt;type:text" json:"resetPasswordExpiresAt"`
	ConfirmationToken     *string    `gorm:"column:confirmationToken;type:text" json:"confirmationToken"`
	ConfirmationExpiresAt *string    `gorm:"column:confirmationExpiresAt;type:text" json:"confirmationExpiresAt"`

	// Relations
	User *User `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (Account) TableName() string { return "account" }

func (a *Account) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID, _ = gonanoid.New()
	}
	if a.AccountID == "" {
		a.AccountID, _ = gonanoid.New()
	}
	return nil
}

// Verification represents the verification table.
type Verification struct {
	ID         string     `gorm:"column:id;primaryKey;type:text" json:"id"`
	Identifier string     `gorm:"column:identifier;type:text;not null" json:"identifier"`
	Value      string     `gorm:"column:value;type:text;not null" json:"value"`
	ExpiresAt  time.Time  `gorm:"column:expires_at;not null" json:"expiresAt"`
	CreatedAt  *time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt  *time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (Verification) TableName() string { return "verification" }

// Organization represents the organization table.
type Organization struct {
	ID        string    `gorm:"column:id;primaryKey;type:text" json:"id"`
	Name      string    `gorm:"column:name;type:text;not null" json:"name"`
	Slug      *string   `gorm:"column:slug;type:text;uniqueIndex:organization_slug_unique" json:"slug"`
	Logo      *string   `gorm:"column:logo;type:text" json:"logo"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	Metadata  *string   `gorm:"column:metadata;type:text" json:"metadata"`
	OwnerID   string    `gorm:"column:owner_id;type:text;not null" json:"ownerId"`

	// Relations
	Owner    *User      `gorm:"foreignKey:OwnerID;references:ID" json:"owner,omitempty"`
	Members  []Member   `gorm:"foreignKey:OrganizationID;references:ID" json:"members"`
	Projects []Project  `gorm:"foreignKey:OrganizationID;references:ID" json:"projects"`
	Servers  []Server   `gorm:"foreignKey:OrganizationID;references:ID" json:"servers"`
}

func (Organization) TableName() string { return "organization" }

func (o *Organization) BeforeCreate(tx *gorm.DB) error {
	if o.ID == "" {
		o.ID, _ = gonanoid.New()
	}
	return nil
}

// Member represents the member table.
type Member struct {
	ID                      string      `gorm:"column:id;primaryKey;type:text" json:"id"`
	OrganizationID          string      `gorm:"column:organization_id;type:text;not null" json:"organizationId"`
	UserID                  string      `gorm:"column:user_id;type:text;not null" json:"userId"`
	Role                    MemberRole  `gorm:"column:role;type:text;not null" json:"role"`
	CreatedAt               time.Time   `gorm:"column:created_at;not null" json:"createdAt"`
	TeamID                  *string     `gorm:"column:team_id;type:text" json:"teamId"`
	IsDefault               bool        `gorm:"column:is_default;not null;default:false" json:"isDefault"`
	CanCreateProjects       bool        `gorm:"column:canCreateProjects;not null;default:false" json:"canCreateProjects"`
	CanAccessToSSHKeys      bool        `gorm:"column:canAccessToSSHKeys;not null;default:false" json:"canAccessToSSHKeys"`
	CanCreateServices       bool        `gorm:"column:canCreateServices;not null;default:false" json:"canCreateServices"`
	CanDeleteProjects       bool        `gorm:"column:canDeleteProjects;not null;default:false" json:"canDeleteProjects"`
	CanDeleteServices       bool        `gorm:"column:canDeleteServices;not null;default:false" json:"canDeleteServices"`
	CanAccessToDocker       bool        `gorm:"column:canAccessToDocker;not null;default:false" json:"canAccessToDocker"`
	CanAccessToAPI          bool        `gorm:"column:canAccessToAPI;not null;default:false" json:"canAccessToAPI"`
	CanAccessToGitProviders bool        `gorm:"column:canAccessToGitProviders;not null;default:false" json:"canAccessToGitProviders"`
	CanAccessToTraefikFiles bool        `gorm:"column:canAccessToTraefikFiles;not null;default:false" json:"canAccessToTraefikFiles"`
	CanDeleteEnvironments   bool        `gorm:"column:canDeleteEnvironments;not null;default:false" json:"canDeleteEnvironments"`
	CanCreateEnvironments   bool        `gorm:"column:canCreateEnvironments;not null;default:false" json:"canCreateEnvironments"`
	AccessedProjects        StringArray `gorm:"column:accesedProjects;type:text[];not null;default:ARRAY[]::text[]" json:"accessedProjects"`
	AccessedEnvironments    StringArray `gorm:"column:accessedEnvironments;type:text[];not null;default:ARRAY[]::text[]" json:"accessedEnvironments"`
	AccessedServices        StringArray `gorm:"column:accesedServices;type:text[];not null;default:ARRAY[]::text[]" json:"accessedServices"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
	User         *User         `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (Member) TableName() string { return "member" }

func (m *Member) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID, _ = gonanoid.New()
	}
	return nil
}

// Invitation represents the invitation table.
type Invitation struct {
	ID             string     `gorm:"column:id;primaryKey;type:text" json:"id"`
	OrganizationID string     `gorm:"column:organization_id;type:text;not null" json:"organizationId"`
	Email          string     `gorm:"column:email;type:text;not null" json:"email"`
	Role           *string    `gorm:"column:role;type:text" json:"role"`
	Status         string     `gorm:"column:status;type:text;not null" json:"status"`
	ExpiresAt      time.Time  `gorm:"column:expires_at;not null" json:"expiresAt"`
	InviterID      string     `gorm:"column:inviter_id;type:text;not null" json:"inviterId"`
	TeamID         *string    `gorm:"column:team_id;type:text" json:"teamId"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null" json:"createdAt"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID;references:ID" json:"organization,omitempty"`
}

func (Invitation) TableName() string { return "invitation" }

// TwoFactor represents the two_factor table.
type TwoFactor struct {
	ID          string `gorm:"column:id;primaryKey;type:text" json:"id"`
	Secret      string `gorm:"column:secret;type:text;not null" json:"secret"`
	BackupCodes string `gorm:"column:backup_codes;type:text;not null" json:"backupCodes"`
	UserID      string `gorm:"column:user_id;type:text;not null" json:"userId"`

	User *User `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (TwoFactor) TableName() string { return "two_factor" }

// APIKey represents the apikey table.
type APIKey struct {
	ID                 string     `gorm:"column:id;primaryKey;type:text" json:"id"`
	Name               *string    `gorm:"column:name;type:text" json:"name"`
	Start              *string    `gorm:"column:start;type:text" json:"start"`
	Prefix             *string    `gorm:"column:prefix;type:text" json:"prefix"`
	Key                string     `gorm:"column:key;type:text;not null" json:"key"`
	UserID             string     `gorm:"column:user_id;type:text;not null" json:"userId"`
	RefillInterval     *int       `gorm:"column:refill_interval" json:"refillInterval"`
	RefillAmount       *int       `gorm:"column:refill_amount" json:"refillAmount"`
	LastRefillAt       *time.Time `gorm:"column:last_refill_at" json:"lastRefillAt"`
	Enabled            *bool      `gorm:"column:enabled" json:"enabled"`
	RateLimitEnabled   *bool      `gorm:"column:rate_limit_enabled" json:"rateLimitEnabled"`
	RateLimitTimeWindow *int      `gorm:"column:rate_limit_time_window" json:"rateLimitTimeWindow"`
	RateLimitMax       *int       `gorm:"column:rate_limit_max" json:"rateLimitMax"`
	RequestCount       *int       `gorm:"column:request_count" json:"requestCount"`
	Remaining          *int       `gorm:"column:remaining" json:"remaining"`
	LastRequest        *time.Time `gorm:"column:last_request" json:"lastRequest"`
	ExpiresAt          *time.Time `gorm:"column:expires_at" json:"expiresAt"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
	Permissions        *string    `gorm:"column:permissions;type:text" json:"permissions"`
	Metadata           *string    `gorm:"column:metadata;type:text" json:"metadata"`

	User *User `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (APIKey) TableName() string { return "apikey" }

// Session represents the session table.
type Session struct {
	ID        string    `gorm:"column:id;primaryKey;type:text" json:"id"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null" json:"expiresAt"`
	Token     string    `gorm:"column:token;type:text;not null;uniqueIndex:session_token_unique" json:"token"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
	IPAddress              *string `gorm:"column:ip_address;type:text" json:"ipAddress"`
	UserAgent              *string `gorm:"column:user_agent;type:text" json:"userAgent"`
	UserID                 string  `gorm:"column:user_id;type:text;not null" json:"userId"`
	ActiveOrganizationID   *string `gorm:"column:active_organization_id;type:text" json:"activeOrganizationId"`

	User *User `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
}

func (Session) TableName() string { return "session" }

func (s *Session) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID, _ = gonanoid.New()
	}
	return nil
}
