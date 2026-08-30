// Pikpik Control Plane Type Definitions (Matching Backend Go DTOs)

// --- Response Envelopes & Meta ---
export interface MetaInfo {
  request_id: string;
  timestamp: string;
}

export interface ApiResponse<T> {
  success: boolean;
  data: T;
  meta: MetaInfo;
}

export interface APIError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
  request_id: string;
  docs_url?: string;
}

export interface ErrorResponse {
  success: boolean;
  error: APIError;
}

// --- Organization & Project Models ---
export interface Organization {
  id: string;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
}

export interface CreateOrgRequest {
  name: string;
  slug?: string;
}

export interface Project {
  id: string;
  org_id: string;
  name: string;
  slug: string;
  description?: string;
  tags: string[];
  app_count: number;
  created_at: string;
  updated_at: string;
}

export interface CreateProjectRequest {
  org_id?: string;
  name: string;
  slug?: string;
  description?: string;
  tags?: string[];
}

export interface UpdateProjectRequest {
  name?: string;
  slug?: string;
  description?: string;
  tags?: string[];
}

export interface TagSummary {
  tag: string;
  count: number;
}

// --- App Models ---
export interface App {
  id: string;
  project_id: string;
  project_name?: string;
  stage_id?: string;
  name: string;
  image: string;
  replicas: number;
  container_port?: number;
  domains: string[];
  tags: string[];
  runtime_mode?: 'swarm' | 'standalone' | string;
  compose_yaml?: string;
  env?: Record<string, string>;
  status: 'running' | 'stopped' | 'deploying' | 'error' | 'pending' | string;
  git_repo_url?: string;
  git_branch?: string;
  dockerfile_path?: string;
  publish_directory?: string;
  build_strategy?: 'dockerfile' | 'nixpacks' | 'compose';
  webhook_secret?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateAppRequest {
  project_id?: string;
  stage_id?: string;
  name: string;
  image?: string;
  replicas: number;
  container_port?: number;
  domains?: string[];
  tags?: string[];
  runtime_mode?: string;
  compose_yaml?: string;
  env?: Record<string, string>;
  git_repo_url?: string;
  git_branch?: string;
  dockerfile_path?: string;
  publish_directory?: string;
  build_strategy?: 'dockerfile' | 'nixpacks' | 'compose';
  webhook_secret?: string;
}

export interface UpdateAppRequest {
  project_id?: string;
  stage_id?: string;
  name?: string;
  image?: string;
  replicas?: number;
  container_port?: number;
  domains?: string[];
  tags?: string[];
  runtime_mode?: string;
  compose_yaml?: string;
  env?: Record<string, string>;
  git_repo_url?: string;
  git_branch?: string;
  dockerfile_path?: string;
  publish_directory?: string;
  build_strategy?: 'dockerfile' | 'nixpacks' | 'compose';
  webhook_secret?: string;
}

export interface DeployAppRequest {
  image?: string;
}

export interface ComposeVariableDef {
  name: string;
  defaultValue?: string;
  isSecret: boolean;
  required: boolean;
}

export interface ComposeServiceInspection {
  name: string;
  image: string;
  ports?: Array<{ hostPort?: number; containerPort?: number; protocol?: string }>;
  volumes?: string[];
  networks?: string[];
  environmentKeys?: string[];
  hasDeployBlock: boolean;
  replicas: number;
}

export interface InspectComposeResponse {
  services: ComposeServiceInspection[];
  variables: ComposeVariableDef[];
  exposed_ports: number[];
  declared_volumes: string[];
  declared_networks: string[];
  suggested_runtime: 'swarm' | 'standalone' | string;
}

// --- Git & Build Models ---
export type BuildStatus =
  | 'queued'
  | 'cloning'
  | 'building'
  | 'deploying'
  | 'success'
  | 'failed'
  | 'cancelled'
  | string;

export interface Build {
  id: string;
  app_id: string;
  service_id?: string;
  deployment_id?: string;
  repo_url?: string;
  commit_sha: string;
  commit_message?: string;
  commit_author?: string;
  author?: string;
  commit_avatar_url?: string;
  branch: string;
  status: 'queued' | 'cloning' | 'building' | 'deploying' | 'success' | 'failed' | 'cancelled' | string;
  logs?: string;
  logs_path?: string;
  duration_ms: number;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
  image_tag?: string;
  error_message?: string;
  strategy?: 'dockerfile' | 'nixpacks' | 'compose' | string;
}

export interface GitHubInstallation {
  id: string;
  org_id: string;
  installation_id: number;
  account_name: string;
  account_type: 'User' | 'Organization' | string;
  repository_selection: 'all' | 'selected' | string;
  permissions?: string;
  created_at: string;
  updated_at: string;
}

// --- Swarm Node Models ---
export interface SwarmNode {
  id: string;
  hostname: string;
  role: 'manager' | 'worker';
  status: 'ready' | 'down' | 'disconnected' | string;
  availability: 'active' | 'pause' | 'drain';
  engine_version: string;
  ip_address: string;
  cpus: number;
  memory_bytes: number;
  leader: boolean;
  updated_at: string;
}

export interface UpdateNodeRequest {
  availability: 'active' | 'pause' | 'drain';
}

export interface JoinTokensResponse {
  manager: string;
  worker: string;
}

// --- Managed Machine Models ---
export interface HostMetrics {
  node_id: string;
  timestamp: string;
  uptime_seconds?: number;
  cpu_percent: number;
  cpu_cores: number;
  load_avg_1m?: number;
  load_avg_5m?: number;
  load_avg_15m?: number;
  mem_total_bytes: number;
  mem_used_bytes: number;
  mem_avail_bytes?: number;
  mem_percent: number;
  swap_total_bytes?: number;
  swap_used_bytes?: number;
  disk_read_bps?: number;
  disk_write_bps?: number;
  disk_read_iops?: number;
  disk_write_iops?: number;
  net_rx_bps?: number;
  net_tx_bps?: number;
  net_rx_errors?: number;
  net_tx_errors?: number;
}

export interface MachineDTO {
  id: string;
  hostname: string;
  role: 'worker' | 'manager' | 'standalone' | string;
  public_ip: string;
  private_ip: string;
  os_kernel: string;
  cpu_arch: string;
  docker_version: string;
  agent_version: string;
  status: 'online' | 'degraded' | 'offline' | string;
  last_seen?: string;
  created_at: string;
  updated_at: string;
  metrics?: HostMetrics;
  containers_count?: number;
}

export interface EnrollMachineResponse {
  command: string;
  token: string;
  server_url: string;
  install_bash: string;
}

export interface JoinSwarmRequest {
  role?: 'worker' | 'manager';
  remote_addrs?: string[];
  join_token?: string;
}

// --- Stack Models ---
export interface StackContainer {
  id: string;
  name: string;
  service: string;
  state: string;
  status: string;
}

export interface Stack {
  id: string;
  project_id?: string;
  name: string;
  compose_yaml: string;
  services: string[];
  status: 'running' | 'deploying' | 'stopped' | 'failed' | string;
  containers?: StackContainer[];
  created_at: string;
  updated_at: string;
}

export interface CreateStackRequest {
  project_id?: string;
  name: string;
  compose_yaml: string;
}

// --- Network Models ---
export interface NetworkDTO {
  id: string;
  project_id: string;
  name: string;
  driver: string;
  scope: string;
  is_external?: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateNetworkRequest {
  project_id?: string;
  name: string;
  driver?: string;
  scope?: string;
  is_external?: boolean;
}

// --- Database Models ---
export type DatabaseEngine = 'postgres' | 'mysql' | 'mariadb' | 'redis' | 'mongodb';

export interface Database {
  id: string;
  name: string;
  engine: DatabaseEngine;
  status: 'running' | 'stopped' | 'starting' | 'error' | string;
  host: string;
  port: number;
  username: string;
  password?: string;
  database_name: string;
  memory_limit_bytes?: number;
  cpu_limit?: number;
  created_at: string;
}

export interface CreateDatabaseRequest {
  name: string;
  engine: DatabaseEngine;
  database_name?: string;
  username?: string;
  password?: string;
  memory_limit_bytes?: number;
  cpu_limit?: number;
}

export interface UpdateDatabaseRequest {
  memory_limit_bytes?: number;
  cpu_limit?: number;
}

// --- Backup Models ---
export interface Backup {
  id: string;
  service_id: string;
  s3_key: string;
  compressed_bytes: number;
  uncompressed_bytes: number;
  duration_ms: number;
  status: 'completed' | 'failed' | 'in_progress' | string;
  created_at: string;
}

export interface CreateBackupRequest {
  service_id: string;
}

export interface RestoreBackupRequest {
  target_service_id?: string;
}

export interface BackupDestination {
  id: string;
  name: string;
  bucket: string;
  region: string;
  endpoint?: string;
  prefix?: string;
  access_key_id: string;
  secret_access_key?: string;
  is_default: boolean;
}

export interface BackupSchedule {
  id: string;
  service_id: string;
  database_type?: string;
  engine?: string;
  cron_expression: string;
  cron_expr?: string;
  s3_destination_id?: string;
  retention_days?: number;
  is_enabled: boolean;
  last_run_at?: string | null;
  next_run_at?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface CreateBackupScheduleRequest {
  service_id: string;
  database_type?: string;
  engine?: string;
  cron_expression: string;
  s3_destination_id?: string;
  retention_days?: number;
  is_enabled?: boolean;
}

export interface UpdateBackupScheduleRequest {
  service_id?: string;
  database_type?: string;
  engine?: string;
  cron_expression?: string;
  s3_destination_id?: string;
  retention_days?: number;
  is_enabled?: boolean;
}

// --- Ingress & Domains ---
export interface DomainBinding {
  id: string;
  app_id: string;
  domain: string;
  auto_tls: boolean;
  status: 'active' | 'pending_dns' | 'provisioning' | 'error' | string;
  created_at: string;
}

export type IngressRoute = DomainBinding;

export interface BindDomainRequest {
  app_id: string;
  domain: string;
  auto_tls: boolean;
}

export interface CertificateUploadRequest {
  domain: string;
  cert_pem: string;
  key_pem: string;
}

export interface CaddyDiagnosticsDTO {
  status: 'online' | 'degraded' | 'offline' | string;
  admin_url: string;
  latency_ms: number;
  active_routes: number;
  config: any;
}

export interface CertificateRecord {
  id: string;
  domain: string;
  type: 'Wildcard' | 'Origin CA' | 'On-Demand' | "Let's Encrypt" | 'Custom';
  issuer: string;
  expires_at?: string;
  expires_in_label: string;
  auto_renew: boolean;
  status: 'active' | 'expiring_soon' | 'expired' | 'pending';
}

export interface Dns01ConfigRequest {
  wildcard_domain: string;
  cloudflare_api_token: string;
  propagation_timeout_sec?: number;
}


// --- Volume Models ---
export interface Volume {
  id: string;
  name: string;
  driver: string;
  scope: 'local' | 'global';
  size_bytes?: number;
  used_by_services?: string[];
  created_at: string;
}

export interface VolumeDTO {
  id: string;
  project_id: string;
  name: string;
  driver: string;
  size_bytes?: number;
  created_at: string;
  updated_at: string;
}

export interface CreateVolumeRequest {
  project_id?: string;
  name: string;
  driver?: string;
}

// --- Registry Models ---
export interface RegistryStatusResponse {
  is_running: boolean;
  container_id?: string;
  storage_bytes: number;
  repositories_count: number;
  last_heartbeat: string;
}

export interface RepositoryCatalogResponse {
  repositories: string[];
  tags?: Record<string, string[]>;
}

export interface RobotCredentialsResponse {
  id: string;
  project_id: string;
  username: string;
  secret_token?: string;
  description?: string;
  created_at: string;
}

export interface CreateRobotRequest {
  project_id: string;
  description?: string;
}

// --- System Models ---
export interface SystemInfo {
  host_os: string;
  docker_version: string;
  swarm_active: boolean;
  nodes_count: number;
  containers_count: number;
  total_memory_bytes: number;
  total_cpus: number;
}

export interface DiskUsageInfo {
  images_bytes: number;
  containers_bytes: number;
  volumes_bytes: number;
  build_cache_bytes: number;
  total_reclaimable_bytes: number;
}

export interface PruneRequest {
  all: boolean;
  volumes: boolean;
  build_cache: boolean;
}

export interface PruneResult {
  space_reclaimed_bytes: number;
  images_deleted?: string[];
  containers_deleted?: string[];
  volumes_deleted?: string[];
}

// --- Auth & User Models ---
export interface LoginRequest {
  email: string;
  password: string;
}

export interface User {
  id: string;
  email: string;
  role: 'owner' | 'admin' | 'developer' | 'viewer' | string;
  created_at: string;
}

export type UserDTO = User;

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: User;
}

export interface CreateTokenRequest {
  name: string;
  scopes: string[];
  expires_at?: string;
}

export interface APIToken {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  raw_secret?: string;
}

export type Token = APIToken;
export type APITokenDTO = APIToken;

// --- Telemetry & Metrics ---
export interface MetricPoint {
  timestamp: number;
  cpu_percent: number;
  memory_bytes: number;
  memory_limit_bytes?: number;
  network_rx_bytes?: number;
  network_tx_bytes?: number;
  disk_read_bytes?: number;
  disk_write_bytes?: number;
}

// --- WebSocket & PTY Framing Models ---
export interface ClientAction {
  action: 'subscribe' | 'unsubscribe' | 'ping';
  channel: 'logs' | 'stats' | 'events' | 'pty';
  target_id: string;
  params?: Record<string, unknown>;
}

export interface WSMessage<T = unknown> {
  channel: string;
  target_id: string;
  event: string;
  data: T;
  timestamp: string;
}

export interface TermResizeMessage {
  cols: number;
  rows: number;
}

export interface TermSignalMessage {
  signal: string;
}

export interface TermExitMessage {
  exit_code: number;
  error?: string;
}

// --- Template & Marketplace Models ---
export interface TemplateEnvVar {
  key: string;
  label: string;
  description: string;
  default?: string;
  required: boolean;
  is_secret: boolean;
  auto_generate?: string; // "hex_32" | "pass_16" | "base64_32" | ""
}

export interface TemplateVolume {
  name: string;
  mount_path: string;
  host_path?: string;
  read_only: boolean;
}

export interface TemplateService {
  name: string;
  image: string;
  command?: string[];
  entrypoint?: string[];
  ports?: Array<{ host_port?: number; container_port: number; protocol?: string }>;
  environment?: Record<string, string>;
  mounts?: TemplateVolume[];
  depends_on?: string[];
  resources?: { cpu_limit?: string; memory_limit?: string };
  health_check?: { test: string[]; interval_sec?: number; timeout_sec?: number; retries?: number };
  restart?: string;
  labels?: Record<string, string>;
}

export interface Template {
  id: string;
  name: string;
  category: string;
  description: string;
  icon: string;
  version: string;
  documentation_url?: string;
  tags?: string[];
  default_port: number;
  services: TemplateService[];
  env_vars?: TemplateEnvVar[];
  volumes?: TemplateVolume[];
  created_at: string;
  updated_at: string;
}

export interface DeployTemplateRequest {
  name?: string;
  project_id?: string;
  stage_id?: string;
  variables?: Record<string, string>;
  domain?: string;
  auto_generate_missing?: boolean;
}

export interface DeployTemplateResponse {
  app_id: string;
  name: string;
  template_id: string;
  category: string;
  status: string;
  services: string[];
  containers?: string[];
  volumes?: string[];
  network: string;
  endpoints?: string[];
  resolved_variables?: Record<string, string>;
  deployed_at: string;
  message?: string;
}

