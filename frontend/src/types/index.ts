export interface User {
  id: number;
  username: string;
  email: string;
  role: string;
  status: "active" | "pending" | "rejected" | string;
  otp_enabled?: boolean;
  created_at?: string;
}

export interface OTPSetupResponse {
  secret: string;
  otpauth_url: string;
  otp_enabled: boolean;
}

export interface RecoverPasswordOTPPayload {
  username_or_email: string;
  code: string;
  new_password: string;
}

export interface PasswordResetRequest {
  id: number;
  user_id: number;
  username: string;
  email: string;
  note?: string;
  status: "pending" | "resolved" | "rejected" | string;
  resolved_by?: string;
  resolved_at?: string;
  created_at: string;
  updated_at: string;
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
  force_recreate?: boolean;
}

export interface ComposeStackYAMLResponse {
  name: string;
  compose_yaml: string;
  source: "database" | "host_file" | "source_file" | "reconstructed" | string;
  config_file?: string;
  working_dir?: string;
  file_path?: string;
  is_read_only?: boolean;
  match_score?: number;
}

export interface UpdateStackYAMLRequest {
  compose_yaml: string;
  redeploy: boolean;
  force_recreate?: boolean;
}

export interface ImageDockerfileResponse {
  image_id: string;
  repo_tags: string[];
  dockerfile: string;
  extend_dockerfile: string;
  source: "database" | "host_file" | "source_file" | "reconstructed" | string;
  context_dir?: string;
  file_path?: string;
  is_read_only?: boolean;
  match_score?: number;
  env?: string[];
  working_dir?: string;
  exposed_ports?: string[];
  entrypoint?: string[];
  cmd?: string[];
}

export interface SourceSearchDir {
  id: number;
  server_id: number;
  path: string;
  recursive: boolean;
  max_depth: number;
  enabled: boolean;
  schedule_enabled: boolean;
  schedule_cron: string;
  last_scanned_at?: string;
  last_scan_error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSourceDirRequest {
  path: string;
  recursive?: boolean;
  max_depth?: number;
  enabled?: boolean;
  schedule_enabled?: boolean;
  schedule_cron?: string;
}

export interface UpdateSourceDirRequest {
  path?: string;
  recursive?: boolean;
  max_depth?: number;
  enabled?: boolean;
  schedule_enabled?: boolean;
  schedule_cron?: string;
}

export interface SourceLink {
  id: number;
  server_id: number;
  kind: "image" | "stack";
  target_ref: string;
  file_path: string;
  file_mtime: number;
  content_hash: string;
  match_score: number;
  status: "auto_matched" | "candidate" | "user_confirmed" | "user_rejected" | "unbound" | "stale" | string;
  metadata?: string;
  created_at: string;
  updated_at: string;
}

export interface CandidateWorkloadRecommendation {
  target_ref: string;
  kind: string;
  name: string;
  score: number;
}

export interface BindSourceRequest {
  link_id?: number;
  file_path?: string;
  kind: "image" | "stack";
  target_ref: string;
}

export interface UnbindSourceRequest {
  link_id?: number;
  file_path?: string;
}

export interface SourceScanProgress {
  stage: "discovering" | "reading" | "matching" | "completed" | "error";
  message: string;
  files_found: number;
  matches_made: number;
  current_path?: string;
  error?: string;
}

export interface ImageUpdateInfo {
  image_id: string;
  repo_tags: string[];
  repository: string;
  current_tag: string;
  current_digest?: string;
  retain_tag?: string;
  retain_digest?: string;
  retain_has_update: boolean;
  full_tag?: string;
  full_digest?: string;
  full_has_update: boolean;
  is_rebuild_digest: boolean;
  current_created_at?: string;
  upstream_created_at?: string;
}

export interface UpdateImageRequest {
  server_id: number;
  image_id: string;
  target_tag: string;
  mode: "retain" | "full";
  recreate_containers: boolean;
  remove_old_image: boolean;
}

export interface UpdateImageResult {
  target_tag: string;
  old_image_id: string;
  new_image_id: string;
  recreated_containers: string[];
  rolled_back: boolean;
  error_message?: string;
}

export interface BuildImageRequest {
  tag: string;
  dockerfile: string;
  no_cache?: boolean;
  pull_parent?: boolean;
  build_args?: Record<string, string>;
  context_dir?: string;
}

export interface DockerBuildMessage {
  stream?: string;
  status?: string;
  progress?: string;
  error?: string;
  errorDetail?: { message?: string };
  aux?: { ID?: string };
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
  server_name?: string;
  package_manager: string;
  last_checked?: string;
  available_updates_count?: number;
  security_updates_count?: number;
  updates_count?: number;
  security_count?: number;
  requires_sudo?: boolean;
  sudo_policy?: string;
  has_saved_sudo_password?: boolean;
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

export interface ActiveUpgradeJob {
  has_active_job: boolean;
  job_id?: string;
  server_id?: number;
  server_name?: string;
  status?: "running" | "success" | "failed";
  package_manager?: string;
  packages?: string[];
  started_at?: string;
  finished_at?: string;
  duration_seconds?: number;
  logs_count?: number;
  result?: PackageUpgradeResult;
}



export interface ProvisionKeyPayload {
  server_host: string;
  server_port?: string;
  ssh_user: string;
  ssh_password: string;
  sudo_password?: string;
  private_key?: string;
}

export interface ProvisionStep {
  name: string;
  status: "ok" | "skipped" | "failed" | "sudo_required";
  message?: string;
}

export interface ProvisionKeyResult {
  success: boolean;
  code?: string;
  message: string;
  steps: ProvisionStep[];
  private_key?: string;
  public_key?: string;
  host_key_fingerprint?: string;
  sudo_required?: boolean;
}

// --- Plugin System Types ---

export interface PluginRuntimeConfig {
  type: "none" | "wasm" | "container";
  entrypoint?: string;
  port?: number;
}

export interface PluginFrontendConfig {
  entrypoint?: string;
}

export interface PluginSidebarExtension {
  id: string;
  title: string;
  icon?: string;
  badge?: string;
}

export interface PluginDashboardExtension {
  id: string;
  title: string;
}

export interface PluginServerTabExtension {
  id: string;
  title: string;
}

export interface PluginUIExtensions {
  sidebar?: PluginSidebarExtension[];
  dashboard_widgets?: PluginDashboardExtension[];
  server_tabs?: PluginServerTabExtension[];
}

export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  homepage?: string;
  license?: string;
  min_kraken_version?: string;
  runtime?: PluginRuntimeConfig;
  frontend?: PluginFrontendConfig;
  permissions?: string[];
  allowed_hosts?: string[];
  ui_extensions?: PluginUIExtensions;
}

export interface InstalledPlugin {
  id: number;
  plugin_id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  homepage?: string;
  license?: string;
  runtime_type: "none" | "wasm" | "container";
  status: "installed" | "enabled" | "disabled" | "error";
  last_error?: string;
  manifest_json: string;
  granted_permissions?: string;
  sha256?: string;
  signature_verified: boolean;
  signer_key_id?: string;
  source_url?: string;
  install_path: string;
  container_id?: string;
  installed_by?: string;
  created_at: string;
  updated_at: string;
}

export interface PluginInspectResponse {
  manifest: PluginManifest;
  sha256: string;
  has_signature: boolean;
  signature_verified: boolean;
  signer_key_id?: string;
  signer_name?: string;
  archive_format: "zip" | "tar.gz";
  size_bytes: number;
  readme_markdown?: string;
  icon_svg?: string;
  needs_compilation?: boolean;
  source_detected?: boolean;
  source_files?: string[];
}

export interface PluginCatalogItem {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  homepage?: string;
  license?: string;
  download_url: string;
  sha256: string;
  icon_url?: string;
  icon_svg?: string;
  readme_markdown?: string;
  runtime_type: "none" | "wasm" | "container";
  permissions: string[];
  ui_extensions?: PluginUIExtensions;
  is_installed?: boolean;
  installed_version?: string;
  has_update?: boolean;
}

export interface PluginCatalogSource {
  id: number;
  name: string;
  url: string;
  enabled: boolean;
  last_synced_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PluginTrustedKey {
  id: number;
  name: string;
  public_key_ed25519: string;
  fingerprint: string;
  added_by?: string;
  created_at: string;
}

export interface PluginCallResponse<T = any> {
  success: boolean;
  result?: T;
  error?: string;
}

