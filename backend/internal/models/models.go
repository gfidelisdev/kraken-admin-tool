package models

import (
	"time"
)

// User represents an administrative user in the database
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"size:128;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         string    `gorm:"size:32;default:'admin';not null" json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

type UserResponse struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
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
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	HostURL    string    `gorm:"size:255;not null" json:"host_url"`
	TLSVerify  bool      `gorm:"default:false" json:"tls_verify"`
	TLSCACert  string    `gorm:"type:text" json:"tls_ca_cert,omitempty"`
	TLSCert    string    `gorm:"type:text" json:"tls_cert,omitempty"`
	TLSKey           string    `gorm:"type:text" json:"-"`
	SSHUser          string    `gorm:"size:128" json:"ssh_user,omitempty"`
	SSHAuthMethod    string    `gorm:"size:32" json:"ssh_auth_method,omitempty"`
	SSHPassword      string    `gorm:"type:text" json:"-"`
	SSHPrivateKey    string    `gorm:"type:text" json:"-"`
	SSHKeyPassphrase string    `gorm:"type:text" json:"-"`
	IsDefault        bool      `gorm:"default:false;index" json:"is_default"`
	Status           string    `gorm:"size:32;default:'UNKNOWN'" json:"status"`
	ErrMsg           string    `gorm:"type:text" json:"err_msg,omitempty"`
	Containers       int       `gorm:"-" json:"containers_count"`
	Images           int       `gorm:"-" json:"images_count"`
	Version          string    `gorm:"size:64" json:"version,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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
	IsDefault        bool   `json:"is_default"`
}

type TestServerRequest struct {
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
