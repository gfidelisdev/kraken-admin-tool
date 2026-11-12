package models

import (
	"time"
)

// User represents an administrative user in the database
type User struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Username         string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Email            string    `gorm:"size:128;uniqueIndex;not null" json:"email"`
	PasswordHash     string    `gorm:"size:255;not null" json:"-"`
	Role             string    `gorm:"size:32;default:'admin';not null" json:"role"`
	Status           string    `gorm:"size:32;default:'active';not null" json:"status"` // "active" | "pending" | "rejected"
	OTPEnabled       bool      `gorm:"default:false" json:"otp_enabled"`
	OTPSecret        string    `gorm:"type:text" json:"-"`
	OTPPendingSecret string    `gorm:"type:text" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// UserDashboardPreference persists custom dashboard layouts per user
type UserDashboardPreference struct {
	UserID     uint      `gorm:"primaryKey" json:"user_id"`
	LayoutJSON string    `gorm:"type:text" json:"layout_json"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UserPluginState tracks per-user enablement status and errors for installed plugins
type UserPluginState struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_user_plugin" json:"user_id"`
	PluginID  string    `gorm:"size:128;not null;uniqueIndex:idx_user_plugin" json:"plugin_id"`
	Status    string    `gorm:"size:32;not null;default:'disabled'" json:"status"` // "enabled" | "disabled" | "error"
	LastError string    `gorm:"type:text" json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PasswordResetRequest represents a request submitted by a user to recover their password via an admin
type PasswordResetRequest struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"index;not null" json:"user_id"`
	Username   string     `gorm:"size:64;not null" json:"username"`
	Email      string     `gorm:"size:128;not null" json:"email"`
	Note       string     `gorm:"type:text" json:"note,omitempty"`
	Status     string     `gorm:"size:32;default:'pending';not null" json:"status"` // "pending" | "resolved" | "rejected"
	ResolvedBy string     `gorm:"size:64" json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// AuditLog tracks administrative actions performed across Docker, K8s, and Auth
type AuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       uint      `json:"user_id"`
	Username     string    `gorm:"size:64;index" json:"username"`
	Action       string    `gorm:"size:64;index" json:"action"`
	ResourceType string    `gorm:"size:64;index" json:"resource_type"`
	ResourceID   string    `gorm:"size:255" json:"resource_id"`
	Details      string    `gorm:"type:text" json:"details"`
	Status       string    `gorm:"size:32" json:"status"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

// DTOs for Auth
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type MicrosoftSessionAccount struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
	Source   string `json:"source"` // "kraken_session"
}

type UserResponse struct {
	ID               uint                     `json:"id"`
	Username         string                   `json:"username"`
	Email            string                   `json:"email"`
	Role             string                   `json:"role"`
	Status           string                   `json:"status"`
	OTPEnabled       bool                     `json:"otp_enabled"`
	CreatedAt        time.Time                `json:"created_at"`
	AuthProvider     string                   `json:"auth_provider,omitempty"`
	MicrosoftAccount *MicrosoftSessionAccount `json:"microsoft_account,omitempty"`
}

type OTPSetupResponse struct {
	Secret     string `json:"secret"`
	AuthURL    string `json:"otpauth_url"`
	OTPEnabled bool   `json:"otp_enabled"`
}

type OTPVerifyRequest struct {
	Code string `json:"code"`
}

type OTPDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type RecoverPasswordOTPRequest struct {
	UsernameOrEmail string `json:"username_or_email"`
	Code            string `json:"code"`
	NewPassword     string `json:"new_password"`
}

type ApproveUserRequest struct {
	Role string `json:"role"`
}

type UpdateUserRoleRequest struct {
	Role string `json:"role"`
}

type AdminResetPasswordRequest struct {
	NewPassword string `json:"new_password,omitempty"`
}

type RecoverPasswordRequest struct {
	UsernameOrEmail string `json:"username_or_email"`
	Note            string `json:"note,omitempty"`
}

type MasterRecoveryRequest struct {
	Username    string `json:"username"`
	NewPassword string `json:"new_password"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

type MicrosoftDeviceCodeRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

type MicrosoftDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type MicrosoftPollRequest struct {
	DeviceCode string `json:"device_code"`
	TenantID   string `json:"tenant_id,omitempty"`
	ClientID   string `json:"client_id,omitempty"`
}

type MicrosoftPollResponse struct {
	Status   string        `json:"status"`
	Token    string        `json:"token,omitempty"`
	User     *UserResponse `json:"user,omitempty"`
	Interval int           `json:"interval,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// DTOs for Docker
type PortSummary struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"private_port"`
	PublicPort  uint16 `json:"public_port,omitempty"`
	Type        string `json:"type"`
}

type ContainerSummary struct {
	ID      string        `json:"id"`
	Names   []string      `json:"names"`
	Image   string        `json:"image"`
	Command string        `json:"command"`
	Created int64         `json:"created"`
	State   string        `json:"state"`
	Status  string        `json:"status"`
	Ports   []PortSummary `json:"ports"`
}

type ImageSummary struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repo_tags"`
	Size     int64    `json:"size"`
	Created  int64    `json:"created"`
}

type PullImageRequest struct {
	Image string `json:"image"`
}

type ImageSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
	IsOfficial  bool   `json:"is_official"`
}

type DockerServer struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	UserID               uint      `gorm:"default:0;index" json:"user_id"`
	Name                 string    `gorm:"size:128;not null" json:"name"`
	HostURL              string    `gorm:"size:255;not null" json:"host_url"`
	TLSVerify            bool      `gorm:"default:false" json:"tls_verify"`
	TLSCACert            string    `gorm:"type:text" json:"tls_ca_cert,omitempty"`
	TLSCert              string    `gorm:"type:text" json:"tls_cert,omitempty"`
	TLSKey               string    `gorm:"type:text" json:"-"`
	SSHUser              string    `gorm:"size:128" json:"ssh_user,omitempty"`
	SSHAuthMethod        string    `gorm:"size:32" json:"ssh_auth_method,omitempty"`
	SSHPassword          string    `gorm:"type:text" json:"-"`
	SSHPrivateKey        string    `gorm:"type:text" json:"-"`
	SSHKeyPassphrase     string    `gorm:"type:text" json:"-"`
	CredentialPolicy     string    `gorm:"size:32;default:'save_securely'" json:"credential_policy"` // "save_securely" | "always_prompt"
	CredentialSource     string    `gorm:"size:32;default:'local'" json:"credential_source"`         // "local" | "plugin" | "vault"
	CredentialPluginID   string    `gorm:"size:128" json:"credential_plugin_id,omitempty"`
	SecretReference      string    `gorm:"size:255" json:"secret_reference,omitempty"`
	SecretVersion        string    `gorm:"size:128" json:"secret_version,omitempty"`
	VaultConfig          string    `gorm:"type:text" json:"vault_config,omitempty"`
	SudoPolicy           string    `gorm:"size:32;default:''" json:"sudo_policy"`                   // "save_securely" | "always_prompt" | ""
	SudoPassword         string    `gorm:"type:text" json:"-"`
	HasSavedSudoPassword bool      `gorm:"-" json:"has_saved_sudo_password"`
	PackageManager        string    `gorm:"size:32" json:"package_manager,omitempty"`
	PackagesLastChecked   *time.Time `json:"packages_last_checked,omitempty"`
	AvailableUpdatesCount int       `gorm:"default:0" json:"available_updates_count"`
	SecurityUpdatesCount  int       `gorm:"default:0" json:"security_updates_count"`
	IsDefault             bool      `gorm:"default:false;index" json:"is_default"`
	Status                string    `gorm:"size:32;default:'UNKNOWN'" json:"status"`
	ErrMsg                string    `gorm:"type:text" json:"err_msg,omitempty"`
	Containers            int       `gorm:"default:0" json:"containers_count"`
	Images                int       `gorm:"default:0" json:"images_count"`
	Version               string    `gorm:"size:64" json:"version,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type HostPackageUpdate struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ServerID       uint      `gorm:"index;not null" json:"server_id"`
	Name           string    `gorm:"size:128;not null" json:"name"`
	CurrentVersion string    `gorm:"size:128" json:"current_version"`
	NewVersion     string    `gorm:"size:128;not null" json:"new_version"`
	Repository     string    `gorm:"size:128" json:"repository,omitempty"`
	IsSecurity     bool      `gorm:"default:false" json:"is_security"`
	Description    string    `gorm:"type:text" json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateServerRequest struct {
	Name             string `json:"name"`
	HostURL          string `json:"host_url"`
	ServerHost       string `json:"server_host,omitempty"`
	ServerPort       string `json:"server_port,omitempty"`
	TLSVerify        bool   `json:"tls_verify"`
	TLSCACert        string `json:"tls_ca_cert"`
	TLSCert          string `json:"tls_cert"`
	TLSKey           string `json:"tls_key"`
	SSHUser          string `json:"ssh_user"`
	SSHAuthMethod    string `json:"ssh_auth_method"`
	SSHPassword      string `json:"ssh_password"`
	SSHPrivateKey    string `json:"ssh_private_key"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase"`
	CredentialPolicy   string `json:"credential_policy,omitempty"` // "save_securely" | "always_prompt"
	CredentialSource   string `json:"credential_source,omitempty"`   // "local" | "plugin" | "vault"
	CredentialPluginID string `json:"credential_plugin_id,omitempty"`
	SecretReference    string `json:"secret_reference,omitempty"`
	SecretVersion      string `json:"secret_version,omitempty"`
	VaultConfig        string `json:"vault_config,omitempty"`
	SudoPolicy         string `json:"sudo_policy,omitempty"`       // "save_securely" | "always_prompt"
	SudoPassword     string `json:"sudo_password,omitempty"`
	PackageManager   string `json:"package_manager,omitempty"`
	IsDefault        bool   `json:"is_default"`
}

type UpdateServerRequest struct {
	Name             string `json:"name"`
	HostURL          string `json:"host_url"`
	ServerHost       string `json:"server_host,omitempty"`
	ServerPort       string `json:"server_port,omitempty"`
	TLSVerify        bool   `json:"tls_verify"`
	TLSCACert        string `json:"tls_ca_cert"`
	TLSCert          string `json:"tls_cert"`
	TLSKey           string `json:"tls_key"`
	SSHUser          string `json:"ssh_user"`
	SSHAuthMethod    string `json:"ssh_auth_method"`
	SSHPassword      string `json:"ssh_password"`
	SSHPrivateKey    string `json:"ssh_private_key"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase"`
	CredentialPolicy   string `json:"credential_policy,omitempty"`
	CredentialSource   string `json:"credential_source,omitempty"`
	CredentialPluginID string `json:"credential_plugin_id,omitempty"`
	SecretReference    string `json:"secret_reference,omitempty"`
	SecretVersion      string `json:"secret_version,omitempty"`
	VaultConfig        string `json:"vault_config,omitempty"`
	SudoPolicy         string `json:"sudo_policy,omitempty"`
	SudoPassword       string `json:"sudo_password,omitempty"`
	PackageManager     string `json:"package_manager,omitempty"`
	IsDefault          bool   `json:"is_default"`
}

type TestServerRequest struct {
	HostURL            string `json:"host_url"`
	ServerHost         string `json:"server_host,omitempty"`
	ServerPort         string `json:"server_port,omitempty"`
	TLSVerify          bool   `json:"tls_verify"`
	TLSCACert          string `json:"tls_ca_cert"`
	TLSCert            string `json:"tls_cert"`
	TLSKey             string `json:"tls_key"`
	SSHUser            string `json:"ssh_user"`
	SSHAuthMethod      string `json:"ssh_auth_method"`
	SSHPassword        string `json:"ssh_password"`
	SSHPrivateKey      string `json:"ssh_private_key"`
	SSHKeyPassphrase   string `json:"ssh_key_passphrase"`
	SudoPassword       string `json:"sudo_password,omitempty"`
	CredentialSource   string `json:"credential_source,omitempty"`
	CredentialPluginID string `json:"credential_plugin_id,omitempty"`
	SecretReference    string `json:"secret_reference,omitempty"`
	SecretVersion      string `json:"secret_version,omitempty"`
	VaultConfig        string `json:"vault_config,omitempty"`
}

// Package Management DTOs
type PackageUpdate struct {
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"`
	NewVersion     string `json:"new_version"`
	Repository     string `json:"repository,omitempty"`
	IsSecurity     bool   `json:"is_security"`
	Description    string `json:"description,omitempty"`
}

type HostPackageStatus struct {
	ServerID              uint            `json:"server_id"`
	ServerName            string          `json:"server_name"`
	PackageManager        string          `json:"package_manager"` // "apt", "dnf", "yum", "apk", "pacman", "zypper", "unknown"
	UpdatesCount          int             `json:"updates_count"`
	SecurityCount         int             `json:"security_count"`
	AvailableUpdatesCount int             `json:"available_updates_count"`
	SecurityUpdatesCount  int             `json:"security_updates_count"`
	Updates               []PackageUpdate `json:"updates"`
	LastChecked           time.Time       `json:"last_checked"`
	RequiresSudo          bool            `json:"requires_sudo"`
	SudoPolicy            string          `json:"sudo_policy"` // "save_securely" | "always_prompt" | ""
	HasSavedSudoPassword  bool            `json:"has_saved_sudo_password"`
}

type CheckPackagesRequest struct {
	SudoPassword   string `json:"sudo_password,omitempty"`
	SaveSudoPolicy string `json:"save_sudo_policy,omitempty"` // "save_securely" | "always_prompt"
	RefreshRepos   bool   `json:"refresh_repos"`
	PackageManager string `json:"package_manager,omitempty"`
}

type UpgradePackagesRequest struct {
	Packages       []string `json:"packages,omitempty"`
	SudoPassword   string   `json:"sudo_password,omitempty"`
	SaveSudoPolicy string   `json:"save_sudo_policy,omitempty"` // "save_securely" | "always_prompt"
	PackageManager string   `json:"package_manager,omitempty"`
}

type UpdateSudoPolicyRequest struct {
	SudoPolicy   string `json:"sudo_policy"` // "save_securely" | "always_prompt"
	SudoPassword string `json:"sudo_password,omitempty"`
}

type UpdatePackageManagerRequest struct {
	PackageManager string `json:"package_manager"`
}

// Docker Compose Stacks DTOs
type ComposeService struct {
	Name             string             `json:"name"`
	Image            string             `json:"image"`
	Status           string             `json:"status"` // "running", "partial", "stopped"
	Replicas         int                `json:"replicas"`
	RunningReplicas  int                `json:"running_replicas"`
	Containers       []ContainerSummary `json:"containers"`
}

type ComposeStack struct {
	Name              string           `json:"name"`
	Status            string           `json:"status"` // "running", "partial", "stopped"
	TotalContainers   int              `json:"total_containers"`
	RunningContainers int              `json:"running_containers"`
	ConfigFiles       string           `json:"config_files,omitempty"`
	WorkingDir        string           `json:"working_dir,omitempty"`
	Created           int64            `json:"created"`
	Services          []ComposeService `json:"services"`
}

type DeployStackRequest struct {
	Name          string            `json:"name"`
	ComposeYAML   string            `json:"compose_yaml"`
	EnvVars       map[string]string `json:"env_vars,omitempty"`
	ForceRecreate bool              `json:"force_recreate,omitempty"`
}

// ComposeStackConfig persists the docker-compose.yaml content for a stack on a given server
type ComposeStackConfig struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ServerID    uint      `gorm:"uniqueIndex:idx_server_stack;not null" json:"server_id"`
	StackName   string    `gorm:"size:128;uniqueIndex:idx_server_stack;not null" json:"stack_name"`
	ComposeYAML string    `gorm:"type:text;not null" json:"compose_yaml"`
	WorkingDir  string    `gorm:"size:512" json:"working_dir,omitempty"`
	ConfigFile  string    `gorm:"size:512" json:"config_file,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// ComposeStackYAMLResponse returned by GET /api/docker/stacks/{name}/yaml
type ComposeStackYAMLResponse struct {
	Name        string  `json:"name"`
	ComposeYAML string  `json:"compose_yaml"`
	Source      string  `json:"source"` // "database" | "host_file" | "reconstructed"
	ConfigFile  string  `json:"config_file,omitempty"`
	WorkingDir  string  `json:"working_dir,omitempty"`
	FilePath    string  `json:"file_path,omitempty"`
	IsReadOnly  bool    `json:"is_read_only"`
	MatchScore  float64 `json:"match_score,omitempty"`
}

// UpdateStackYAMLRequest used by PUT /api/docker/stacks/{name}/yaml
type UpdateStackYAMLRequest struct {
	ComposeYAML   string `json:"compose_yaml"`
	Redeploy      bool   `json:"redeploy"`
	ForceRecreate bool   `json:"force_recreate,omitempty"`
}

// ImageDockerfileConfig persists the Dockerfile content for locally built/edited images
type ImageDockerfileConfig struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ServerID   uint      `gorm:"index;not null" json:"server_id"`
	ImageTag   string    `gorm:"size:255;index;not null" json:"image_tag"`
	ImageID    string    `gorm:"size:128;index" json:"image_id"`
	Dockerfile string    `gorm:"type:text;not null" json:"dockerfile"`
	ContextDir string    `gorm:"size:512" json:"context_dir,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// ImageDockerfileResponse returned by GET /api/docker/images/dockerfile
type ImageDockerfileResponse struct {
	ImageID          string   `json:"image_id"`
	RepoTags         []string `json:"repo_tags"`
	Dockerfile       string   `json:"dockerfile"`
	ExtendDockerfile string   `json:"extend_dockerfile"`
	Source           string   `json:"source"` // "database" | "host_file" | "reconstructed"
	ContextDir       string   `json:"context_dir,omitempty"`
	FilePath         string   `json:"file_path,omitempty"`
	IsReadOnly       bool     `json:"is_read_only"`
	MatchScore       float64  `json:"match_score,omitempty"`
	Env              []string `json:"env,omitempty"`
	WorkingDir       string   `json:"working_dir,omitempty"`
	ExposedPorts     []string `json:"exposed_ports,omitempty"`
	Entrypoint       []string `json:"entrypoint,omitempty"`
	Cmd              []string `json:"cmd,omitempty"`
}

// SourceSearchDir represents an administrator-defined directory on a target host to scan for source files
type SourceSearchDir struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ServerID        uint       `gorm:"index;not null" json:"server_id"`
	Path            string     `gorm:"size:512;not null" json:"path"`
	Recursive       bool       `gorm:"default:true" json:"recursive"`
	MaxDepth        int        `gorm:"default:5" json:"max_depth"`
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	ScheduleEnabled bool       `gorm:"default:false" json:"schedule_enabled"`
	ScheduleCron    string     `gorm:"size:64;default:'@daily'" json:"schedule_cron"`
	LastScannedAt   *time.Time `json:"last_scanned_at,omitempty"`
	LastScanError   string     `gorm:"type:text" json:"last_scan_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CreateSourceDirRequest used to add a new search directory
type CreateSourceDirRequest struct {
	Path            string `json:"path"`
	Recursive       *bool  `json:"recursive,omitempty"`
	MaxDepth        int    `json:"max_depth,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	ScheduleEnabled *bool  `json:"schedule_enabled,omitempty"`
	ScheduleCron    string `json:"schedule_cron,omitempty"`
}

// UpdateSourceDirRequest used to update search directory settings
type UpdateSourceDirRequest struct {
	Path            string `json:"path,omitempty"`
	Recursive       *bool  `json:"recursive,omitempty"`
	MaxDepth        int    `json:"max_depth,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	ScheduleEnabled *bool  `json:"schedule_enabled,omitempty"`
	ScheduleCron    string `json:"schedule_cron,omitempty"`
}

// SourceLink represents a reverse-engineered relationship between a Docker image/stack and a host source file
type SourceLink struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Kind        string    `gorm:"size:32;index;not null" json:"kind"` // "image" | "stack"
	TargetRef   string    `gorm:"size:255;index" json:"target_ref"`  // image tag/ID or stack name (empty if unbound)
	FilePath    string    `gorm:"size:512;not null" json:"file_path"`
	FileMTime   int64     `gorm:"column:file_mtime" json:"file_mtime"`
	ContentHash string    `gorm:"size:64;not null" json:"content_hash"`
	MatchScore  float64   `gorm:"default:0" json:"match_score"`
	Status      string    `gorm:"size:32;default:'unbound'" json:"status"` // "auto_matched" | "candidate" | "user_confirmed" | "user_rejected" | "unbound" | "stale"
	Metadata    string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BindSourceRequest allows administrators to manually bind a discovered host source file to a workload
type BindSourceRequest struct {
	LinkID    uint   `json:"link_id,omitempty"`
	FilePath  string `json:"file_path,omitempty"`
	Kind      string `json:"kind"`       // "image" | "stack"
	TargetRef string `json:"target_ref"` // image tag/ID or stack name
}

// UnbindSourceRequest unbinds a source file from a workload
type UnbindSourceRequest struct {
	LinkID   uint   `json:"link_id,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

// SourceScanProgress represents real-time progress events sent during a scan
type SourceScanProgress struct {
	Stage       string `json:"stage"` // "discovering", "reading", "matching", "completed", "error"
	Message     string `json:"message"`
	FilesFound  int    `json:"files_found"`
	MatchesMade int    `json:"matches_made"`
	CurrentPath string `json:"current_path,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ImageUpdateInfo represents Docker Hub update checks for an image
type ImageUpdateInfo struct {
	ImageID           string    `json:"image_id"`
	RepoTags          []string  `json:"repo_tags"`
	Repository        string    `json:"repository"`
	CurrentTag        string    `json:"current_tag"`
	CurrentDigest     string    `json:"current_digest,omitempty"`
	RetainTag         string    `json:"retain_tag,omitempty"`
	RetainDigest      string    `json:"retain_digest,omitempty"`
	RetainHasUpdate   bool      `json:"retain_has_update"`
	FullTag           string    `json:"full_tag,omitempty"`
	FullDigest        string    `json:"full_digest,omitempty"`
	FullHasUpdate     bool      `json:"full_has_update"`
	IsRebuildDigest   bool      `json:"is_rebuild_digest"` // true if upstream has rebuilt the same tag with security patches
	CurrentCreatedAt  time.Time `json:"current_created_at,omitempty"`
	UpstreamCreatedAt time.Time `json:"upstream_created_at,omitempty"`
}

// UpdateImageRequest used to trigger an image update
type UpdateImageRequest struct {
	ServerID           uint   `json:"server_id"`
	ImageID            string `json:"image_id"`
	TargetTag          string `json:"target_tag"` // e.g. "redis:7.0.14-alpine"
	Mode               string `json:"mode"`       // "retain" | "full"
	RecreateContainers bool   `json:"recreate_containers"`
	RemoveOldImage     bool   `json:"remove_old_image"`
}

// BuildImageRequest used by POST /api/docker/images/build and /api/docker/images/build/stream
type BuildImageRequest struct {
	Tag        string            `json:"tag"`
	Dockerfile string            `json:"dockerfile"`
	NoCache    bool              `json:"no_cache,omitempty"`
	PullParent bool              `json:"pull_parent,omitempty"`
	BuildArgs  map[string]string `json:"build_args,omitempty"`
	ContextDir string            `json:"context_dir,omitempty"`
}

// DTOs for K8s
type PodSummary struct {
	Name       string   `json:"name"`
	Namespace  string   `json:"namespace"`
	Status     string   `json:"status"`
	Restarts   int32    `json:"restarts"`
	Age        string   `json:"age"`
	Node       string   `json:"node"`
	IP         string   `json:"ip"`
	Containers []string `json:"containers"`
}

type DeploymentSummary struct {
	Name              string   `json:"name"`
	Namespace         string   `json:"namespace"`
	ReadyReplicas     int32    `json:"ready_replicas"`
	DesiredReplicas   int32    `json:"desired_replicas"`
	AvailableReplicas int32    `json:"available_replicas"`
	Age               string   `json:"age"`
	Images            []string `json:"images"`
}

type ScaleRequest struct {
	Replicas int32 `json:"replicas"`
}

// DTOs for System Status
type SystemStatusResponse struct {
	Version              string `json:"version"`
	Uptime               string `json:"uptime"`
	DockerConnected      bool   `json:"docker_connected"`
	DockerVersion        string `json:"docker_version,omitempty"`
	DockerContainers     int    `json:"docker_containers"`
	DockerImages         int    `json:"docker_images"`
	K8sConnected         bool   `json:"k8s_connected"`
	K8sVersion           string `json:"k8s_version,omitempty"`
	K8sPods              int    `json:"k8s_pods"`
	K8sDeployments       int    `json:"k8s_deployments"`
	DatabaseType         string `json:"database_type"`
	DatabaseStatus       string `json:"database_status"`
}

// --- Plugin System Models & DTOs ---

// InstalledPlugin represents a plugin installed in Kraken
type InstalledPlugin struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	PluginID           string    `gorm:"size:128;uniqueIndex;not null" json:"plugin_id"`
	Name               string    `gorm:"size:128;not null" json:"name"`
	Version            string    `gorm:"size:64;not null" json:"version"`
	Description        string    `gorm:"type:text" json:"description"`
	Author             string    `gorm:"size:128" json:"author"`
	Homepage           string    `gorm:"size:255" json:"homepage"`
	License            string    `gorm:"size:64" json:"license"`
	RuntimeType        string    `gorm:"size:32;default:'none';not null" json:"runtime_type"` // "none" | "wasm" | "container"
	Status             string    `gorm:"size:32;default:'installed';not null" json:"status"`   // "installed" | "enabled" | "disabled" | "error"
	LastError          string    `gorm:"type:text" json:"last_error,omitempty"`
	ManifestJSON       string    `gorm:"type:text;not null" json:"manifest_json"`
	GrantedPermissions string    `gorm:"type:text" json:"granted_permissions"` // JSON array
	SHA256             string    `gorm:"size:64" json:"sha256"`
	SignatureVerified  bool      `gorm:"default:false" json:"signature_verified"`
	SignerKeyID        string    `gorm:"size:128" json:"signer_key_id,omitempty"`
	SourceURL          string    `gorm:"type:text" json:"source_url,omitempty"`
	InstallPath        string    `gorm:"type:text" json:"install_path"`
	ContainerID        string     `gorm:"size:128" json:"container_id,omitempty"`
	InstallationID     string     `gorm:"size:64;index" json:"installation_id,omitempty"`
	InstanceSecret     string     `gorm:"type:text" json:"-"` // Never expose in JSON responses / Web Workers
	PreviousSecret     string     `gorm:"type:text" json:"-"` // Used for graceful secret rotation window
	SecretRotatedAt    *time.Time `json:"-"`
	InstalledBy        string     `gorm:"size:64" json:"installed_by,omitempty"`
	IconURL            string    `gorm:"-" json:"icon_url,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// PluginKVStore provides isolated key-value storage for plugins
type PluginKVStore struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	PluginID  string    `gorm:"size:128;index:idx_plugin_user_kv,unique;not null" json:"plugin_id"`
	Key       string    `gorm:"size:255;index:idx_plugin_user_kv,unique;not null" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UserID    uint      `gorm:"default:0;index:idx_plugin_user_kv,unique;not null" json:"user_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PluginCatalogSource represents an online repository of plugins (index.json)
type PluginCatalogSource struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Name            string     `gorm:"size:128;not null" json:"name"`
	URL             string     `gorm:"size:512;uniqueIndex;not null" json:"url"`
	Enabled         bool       `gorm:"default:true" json:"enabled"`
	LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
	CachedIndexJSON string     `gorm:"type:text" json:"cached_index_json,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// PluginTrustedKey represents a trusted Ed25519 public key for verifying signed plugins
type PluginTrustedKey struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Name             string    `gorm:"size:128;not null" json:"name"`
	PublicKeyEd25519 string    `gorm:"type:text;not null" json:"public_key_ed25519"`
	Fingerprint      string    `gorm:"size:64;uniqueIndex;not null" json:"fingerprint"`
	AddedBy          string    `gorm:"size:64" json:"added_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// PluginManifest represents the deserialized plugin.json manifest
type PluginManifest struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Version          string               `json:"version"`
	Description      string               `json:"description"`
	Author           string               `json:"author"`
	Homepage         string               `json:"homepage,omitempty"`
	License          string               `json:"license,omitempty"`
	MinKrakenVersion string               `json:"min_kraken_version,omitempty"`
	Runtime          PluginRuntimeConfig  `json:"runtime,omitempty"`
	Frontend         PluginFrontendConfig `json:"frontend,omitempty"`
	Permissions      []string             `json:"permissions,omitempty"`
	AllowedHosts     []string             `json:"allowed_hosts,omitempty"`
	UIExtensions     PluginUIExtensions   `json:"ui_extensions,omitempty"`
}

type PluginRuntimeConfig struct {
	Type       string `json:"type"`                 // "none" | "wasm" | "container"
	Entrypoint string `json:"entrypoint,omitempty"` // e.g. "backend/plugin.wasm" or image name
	Dockerfile string `json:"dockerfile,omitempty"` // e.g. "Dockerfile" or "backend/Dockerfile"
	Port       int    `json:"port,omitempty"`       // container port, e.g. 8080
}

type PluginFrontendConfig struct {
	Entrypoint string `json:"entrypoint,omitempty"` // e.g. "frontend/worker.js"
}

type PluginUIExtensions struct {
	Sidebar          []PluginSidebarExtension   `json:"sidebar,omitempty"`
	DashboardWidgets []PluginDashboardExtension `json:"dashboard_widgets,omitempty"`
	ServerTabs       []PluginServerTabExtension `json:"server_tabs,omitempty"`
}

type PluginSidebarExtension struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Badge string `json:"badge,omitempty"`
}

type PluginDashboardExtension struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type PluginServerTabExtension struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DTOs for Plugin Store & Install
type PluginInspectResponse struct {
	Manifest          PluginManifest `json:"manifest"`
	SHA256            string         `json:"sha256"`
	HasSignature      bool           `json:"has_signature"`
	SignatureVerified bool           `json:"signature_verified"`
	SignerKeyID       string         `json:"signer_key_id,omitempty"`
	SignerName        string         `json:"signer_name,omitempty"`
	ArchiveFormat     string         `json:"archive_format"` // "zip" | "tar.gz"
	SizeBytes         int64          `json:"size_bytes"`
	ReadmeMarkdown    string         `json:"readme_markdown,omitempty"`
	IconSVG           string         `json:"icon_svg,omitempty"`
	IconURL           string         `json:"icon_url,omitempty"`
}

type InstallPluginRequest struct {
	URL                string   `json:"url,omitempty"`
	Path               string   `json:"path,omitempty"`
	ExpectedSHA256     string   `json:"expected_sha256,omitempty"`
	GrantedPermissions []string `json:"granted_permissions,omitempty"`
	EnableImmediately  bool     `json:"enable_immediately,omitempty"`
}

type InspectDirectoryRequest struct {
	Path string `json:"path"`
}

type PluginCallRequest struct {
	Action  string      `json:"action"`
	Payload interface{} `json:"payload,omitempty"`
}

type PluginCallResponse struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type PluginCatalogIndex struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	UpdatedAt   string              `json:"updated_at,omitempty"`
	Plugins     []PluginCatalogItem `json:"plugins"`
}

type PluginCatalogItem struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Version          string               `json:"version"`
	Description      string               `json:"description"`
	Author           string               `json:"author"`
	Homepage         string               `json:"homepage,omitempty"`
	License          string               `json:"license,omitempty"`
	DownloadURL      string               `json:"download_url"`
	SHA256           string               `json:"sha256"`
	IconURL          string               `json:"icon_url,omitempty"`
	IconSVG          string               `json:"icon_svg,omitempty"`
	ReadmeMarkdown   string               `json:"readme_markdown,omitempty"`
	RuntimeType      string               `json:"runtime_type"`
	Permissions      []string             `json:"permissions"`
	UIExtensions     PluginUIExtensions   `json:"ui_extensions,omitempty"`
	IsInstalled      bool                 `json:"is_installed,omitempty"`
	InstalledVersion string               `json:"installed_version,omitempty"`
	HasUpdate        bool                 `json:"has_update,omitempty"`
}
