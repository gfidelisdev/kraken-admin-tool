export interface User {
  id: number;
  username: string;
  email: string;
  role: string;
  created_at?: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

export interface PortSummary {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export interface DockerContainer {
  id: string;
  names: string[];
  image: string;
  command: string;
  created: number;
  state: string; // "running", "exited", "created", "paused", "restarting"
  status: string;
  ports: PortSummary[];
}

export interface DockerImage {
  id: string;
  repo_tags: string[];
  size: number;
  created: number;
}

export interface ImageSearchResult {
  name: string;
  description: string;
  star_count: number;
  is_official: boolean;
}

export interface DockerPullProgress {
  current?: number;
  total?: number;
  start?: number;
}

export interface DockerPullMessage {
  status?: string;
  progressDetail?: DockerPullProgress;
  progress?: string;
  id?: string;
  error?: string;
  errorDetail?: { message?: string };
}

export interface DockerServer {
  id: number;
  name: string;
  host_url: string;
  tls_verify: boolean;
  tls_ca_cert?: string;
  tls_cert?: string;
  ssh_user?: string;
  ssh_auth_method?: "key" | "password" | string;
  credential_policy?: "save_securely" | "always_prompt" | string;
  sudo_policy?: "save_securely" | "always_prompt" | string;
  has_saved_sudo_password?: boolean;
  package_manager?: string;
  packages_last_checked?: string;
  available_updates_count?: number;
  security_updates_count?: number;
  is_default: boolean;
  status: "CONNECTED" | "UNREACHABLE" | "UNKNOWN" | string;
  err_msg?: string;
  containers_count: number;
  images_count: number;
  version?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateServerPayload {
  name: string;
  host_url: string;
  server_host?: string;
  server_port?: string;
  tls_verify?: boolean;
  tls_ca_cert?: string;
  tls_cert?: string;
  tls_key?: string;
  ssh_user?: string;
  ssh_auth_method?: "key" | "password" | string;
  ssh_password?: string;
  ssh_private_key?: string;
  ssh_key_passphrase?: string;
  credential_policy?: "save_securely" | "always_prompt" | string;
  sudo_policy?: "save_securely" | "always_prompt" | string;
  sudo_password?: string;
  package_manager?: string;
  is_default?: boolean;
}

export interface TestServerPayload {
  host_url?: string;
  server_host?: string;
  server_port?: string;
  tls_verify?: boolean;
  tls_ca_cert?: string;
  tls_cert?: string;
  tls_key?: string;
  ssh_user?: string;
  ssh_auth_method?: "key" | "password" | string;
  ssh_password?: string;
  ssh_private_key?: string;
  ssh_key_passphrase?: string;
}

export interface ServerDiagnosticLog {
  level: "info" | "success" | "warning" | "error";
  message: string;
  timestamp: string;
  suggestion?: string;
  extra?: Record<string, any>;
}

export interface TestServerResult {
  status: "CONNECTED" | "UNREACHABLE";
  version?: string;
  api_version?: string;
  os_arch?: string;
  message?: string;
  error?: string;
  logs?: ServerDiagnosticLog[];
}

export interface K8sPod {
  name: string;
  namespace: string;
  status: string; // "Running", "Pending", "Failed", "Succeeded", "CrashLoopBackOff"
  restarts: number;
  age: string;
  node: string;
  ip: string;
  containers: string[];
}

export interface K8sDeployment {
  name: string;
  namespace: string;
  ready_replicas: number;
  desired_replicas: number;
  available_replicas: number;
  age: string;
  images: string[];
}

export interface AuditLog {
  id: number;
  user_id: number;
  username: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details: string;
  status: "SUCCESS" | "FAILED" | string;
  created_at: string;
}

export interface SystemStatus {
  version: string;
  uptime: string;
  docker_connected: boolean;
  docker_version?: string;
  docker_containers: number;
  docker_images: number;
  k8s_connected: boolean;
  k8s_version?: string;
  k8s_pods: number;
  k8s_deployments: number;
  database_type: string;
  database_status: string;
}

// Docker Compose Stack Types
export interface ComposeService {
  name: string;
  image: string;
  replicas: number;
  running_replicas: number;
  containers: DockerContainer[];
}

export interface ComposeStack {
  name: string;
  status: "running" | "stopped" | "partially_running" | string;
  services: ComposeService[];
  total_containers: number;
  running_containers: number;
  config_files?: string;
  working_dir?: string;
  created: number;
}

export interface DeployStackRequest {
  name: string;
  compose_yaml: string;
}

// Host Package Management Types
export interface PackageUpdate {
  name: string;
  current_version: string;
  new_version: string;
  repository?: string;
  is_security: boolean;
  description?: string;
}

export interface HostPackageStatus {
  server_id: number;
  package_manager: string;
  last_checked?: string;
  available_updates_count: number;
  security_updates_count: number;
  updates?: PackageUpdate[] | null;
}

export interface CheckPackagesRequest {
  sudo_password?: string;
  package_manager?: string;
}

export interface UpgradePackagesRequest {
  packages?: string[];
  sudo_password?: string;
  package_manager?: string;
}

export interface UpdateSudoPolicyRequest {
  sudo_policy: "save_securely" | "always_prompt" | "";
  sudo_password?: string;
}

export interface UpdatePackageManagerRequest {
  package_manager: string;
}

export interface PackageUpgradeLog {
  type?: "log";
  message: string;
  level: "info" | "stdout" | "stderr" | "error";
  timestamp: string;
}

export interface PackageUpgradeResult {
  success: boolean;
  message: string;
  output?: string;
  remaining_updates_count?: number;
  security_updates_count?: number;
  error?: string;
  code?: string;
}

