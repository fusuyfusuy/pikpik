import {
  ApiResponse,
  APIError,
  App,
  CreateAppRequest,
  UpdateAppRequest,
  DeployAppRequest,
  SetTrafficSplitRequest,
  TrafficSplitResponse,
  SwarmNode,
  UpdateNodeRequest,
  JoinTokensResponse,
  Stack,
  CreateStackRequest,
  Database,
  CreateDatabaseRequest,
  UpdateDatabaseRequest,
  Backup,
  BackupDestination,
  DomainBinding,
  BindDomainRequest,
  CertificateUploadRequest,
  CaddyDiagnosticsDTO,
  RegistryStatusResponse,
  RepositoryCatalogResponse,
  RobotCredentialsResponse,
  SystemInfo,
  DiskUsageInfo,
  PruneRequest,
  PruneResult,
  User,
  LoginRequest,
  LoginResponse,
  APIToken,
  CreateTokenRequest,
  Build,
  Template,
  DeployTemplateRequest,
  DeployTemplateResponse,
  BackupSchedule,
  CreateBackupScheduleRequest,
  UpdateBackupScheduleRequest,
  Organization,
  CreateOrgRequest,
  Project,
  CreateProjectRequest,
  UpdateProjectRequest,
  TagSummary,
  InspectComposeResponse,
  NetworkDTO,
  CreateNetworkRequest,
  VolumeDTO,
  CreateVolumeRequest,
  MachineDTO,
  HostMetrics,
  EnrollMachineResponse,
  JoinSwarmRequest,
  NotificationChannel,
  CreateNotificationChannelRequest,
  TeamUser,
  InviteUserRequest,
  TeamInvitationResponse,
  AcceptInviteRequest,
  UpdateUserRoleRequest,
  ResetPasswordRequest,
  ProjectMember,
  SetProjectMemberRequest,
  Integration,
  CreateIntegrationRequest,
  UpdateIntegrationRequest,
  TestIntegrationResponse,
} from './types';

const TOKEN_KEY = 'pikpik_token';
const API_BASE = '';

export const getToken = (): string | null => {
  return localStorage.getItem(TOKEN_KEY);
};

export const setToken = (token: string): void => {
  localStorage.setItem(TOKEN_KEY, token);
};

export const removeToken = (): void => {
  localStorage.removeItem(TOKEN_KEY);
};

export class ApiClientError extends Error {
  code: string;
  requestId?: string;
  details?: Record<string, unknown>;

  constructor(error: APIError) {
    super(error.message);
    this.name = 'ApiClientError';
    this.code = error.code;
    this.requestId = error.request_id;
    this.details = error.details;
  }
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers,
  });

  if (response.status === 204) {
    return {} as T;
  }

  const json = await response.json();

  if (!response.ok || !json.success) {
    if (response.status === 401) {
      removeToken();
    }
    const apiErr = json.error || {
      code: 'INTERNAL_ERROR',
      message: response.statusText || 'An unexpected error occurred',
      request_id: json.meta?.request_id || '',
    };
    throw new ApiClientError(apiErr);
  }

  const data = (json as ApiResponse<T>).data;
  return (data !== undefined ? data : null) as T;
}

export const api = {
  // --- Auth ---
  auth: {
    login: async (req: LoginRequest): Promise<LoginResponse> => {
      const res = await request<LoginResponse>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify(req),
      });
      if (res.token) {
        setToken(res.token);
      }
      return res;
    },
    logout: async (): Promise<void> => {
      try {
        await request('/api/v1/auth/logout', { method: 'POST' });
      } finally {
        removeToken();
      }
    },
    me: (): Promise<User> => request<User>('/api/v1/auth/me'),
    passkeyBegin: (): Promise<{ challenge: string }> =>
      request('/api/v1/auth/passkey/login/begin', { method: 'POST' }),
    passkeyFinish: async (data: unknown): Promise<{ token: string }> => {
      const res = await request<{ token: string }>('/api/v1/auth/passkey/login/finish', {
        method: 'POST',
        body: JSON.stringify(data),
      });
      if (res.token) {
        setToken(res.token);
      }
      return res;
    },
    listTokens: (): Promise<APIToken[]> => request<APIToken[]>('/api/v1/auth/tokens'),
    createToken: (req: CreateTokenRequest): Promise<APIToken> =>
      request<APIToken>('/api/v1/auth/tokens', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    deleteToken: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/auth/tokens/${id}`, { method: 'DELETE' }),
  },

  // --- Organizations ---
  orgs: {
    list: (): Promise<Organization[]> => request<Organization[]>('/api/v1/orgs'),
    create: (req: CreateOrgRequest): Promise<Organization> =>
      request<Organization>('/api/v1/orgs', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Projects ---
  projects: {
    list: (params?: { org_id?: string; tag?: string }): Promise<Project[]> => {
      const qs = new URLSearchParams();
      if (params?.org_id) qs.set('org_id', params.org_id);
      if (params?.tag) qs.set('tag', params.tag);
      const query = qs.toString() ? `?${qs.toString()}` : '';
      return request<Project[]>(`/api/v1/projects${query}`);
    },
    get: (id: string): Promise<Project> => request<Project>(`/api/v1/projects/${id}`),
    create: (req: CreateProjectRequest): Promise<Project> =>
      request<Project>('/api/v1/projects', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: UpdateProjectRequest): Promise<Project> =>
      request<Project>(`/api/v1/projects/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/projects/${id}`, { method: 'DELETE' }),
  },

  // --- Tags ---
  tags: {
    list: (): Promise<TagSummary[]> => request<TagSummary[]>('/api/v1/tags'),
  },

  // --- Apps ---
  apps: {
    list: (params?: { project_id?: string; tag?: string; search?: string }): Promise<App[]> => {
      const qs = new URLSearchParams();
      if (params?.project_id) qs.set('project_id', params.project_id);
      if (params?.tag) qs.set('tag', params.tag);
      if (params?.search) qs.set('search', params.search);
      const query = qs.toString() ? `?${qs.toString()}` : '';
      return request<App[]>(`/api/v1/apps${query}`);
    },
    get: (id: string): Promise<App> => request<App>(`/api/v1/apps/${id}`),
    create: (req: CreateAppRequest): Promise<App> =>
      request<App>('/api/v1/apps', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    inspectCompose: (composeYAML: string): Promise<InspectComposeResponse> =>
      request<InspectComposeResponse>('/api/v1/apps/inspect-compose', {
        method: 'POST',
        body: JSON.stringify({ compose_yaml: composeYAML }),
      }),
    update: (id: string, req: UpdateAppRequest): Promise<App> =>
      request<App>(`/api/v1/apps/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/apps/${id}`, { method: 'DELETE' }),
    deploy: (id: string, req?: DeployAppRequest): Promise<{ status: string }> =>
      request(`/api/v1/apps/${id}/deploy`, {
        method: 'POST',
        body: JSON.stringify(req || {}),
      }),
    restart: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/apps/${id}/restart`, { method: 'POST' }),
    stop: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/apps/${id}/stop`, { method: 'POST' }),
    start: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/apps/${id}/start`, { method: 'POST' }),
    getEnv: (id: string): Promise<Record<string, string>> =>
      request<Record<string, string>>(`/api/v1/apps/${id}/env`),
    updateEnv: (id: string, env: Record<string, string>): Promise<{ status: string }> =>
      request(`/api/v1/apps/${id}/env`, {
        method: 'PUT',
        body: JSON.stringify(env),
      }),
    getTraffic: (id: string): Promise<TrafficSplitResponse> =>
      request<TrafficSplitResponse>(`/api/v1/apps/${id}/traffic`),
    setTraffic: (id: string, req: SetTrafficSplitRequest): Promise<TrafficSplitResponse> =>
      request<TrafficSplitResponse>(`/api/v1/apps/${id}/traffic`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Stacks ---
  stacks: {
    list: (): Promise<Stack[]> => request<Stack[]>('/api/v1/stacks'),
    get: (id: string): Promise<Stack> => request<Stack>(`/api/v1/stacks/${id}`),
    create: (req: CreateStackRequest): Promise<Stack> =>
      request<Stack>('/api/v1/stacks', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: Partial<CreateStackRequest>): Promise<Stack> =>
      request<Stack>(`/api/v1/stacks/${id}`, {
        method: 'PUT',
        body: JSON.stringify(req),
      }),
    deploy: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/stacks/${id}/deploy`, { method: 'POST' }),
    restart: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/stacks/${id}/restart`, { method: 'POST' }),
    stop: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/stacks/${id}/stop`, { method: 'POST' }),
    logs: (id: string): Promise<any> =>
      request(`/api/v1/stacks/${id}/logs`),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/stacks/${id}`, { method: 'DELETE' }),
  },

  // --- Networks ---
  networks: {
    list: (projectId?: string): Promise<NetworkDTO[]> => {
      const qs = projectId ? `?project_id=${projectId}` : '';
      return request<NetworkDTO[]>(`/api/v1/networks${qs}`);
    },
    get: (id: string): Promise<NetworkDTO> => request<NetworkDTO>(`/api/v1/networks/${id}`),
    create: (req: CreateNetworkRequest): Promise<NetworkDTO> =>
      request<NetworkDTO>('/api/v1/networks', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/networks/${id}`, { method: 'DELETE' }),
    prune: (projectId?: string): Promise<{ deleted: string[] }> => {
      const qs = projectId ? `?project_id=${projectId}` : '';
      return request(`/api/v1/networks/prune${qs}`, { method: 'POST' });
    },
  },

  // --- Volumes ---
  volumes: {
    list: (projectId?: string): Promise<VolumeDTO[]> => {
      const qs = projectId ? `?project_id=${projectId}` : '';
      return request<VolumeDTO[]>(`/api/v1/volumes${qs}`);
    },
    get: (id: string): Promise<VolumeDTO> => request<VolumeDTO>(`/api/v1/volumes/${id}`),
    create: (req: CreateVolumeRequest): Promise<VolumeDTO> =>
      request<VolumeDTO>('/api/v1/volumes', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/volumes/${id}`, { method: 'DELETE' }),
    prune: (projectId?: string): Promise<{ deleted: string[]; space_reclaimed?: number }> => {
      const qs = projectId ? `?project_id=${projectId}` : '';
      return request(`/api/v1/volumes/prune${qs}`, { method: 'POST' });
    },
  },

  // --- Swarm Nodes ---
  nodes: {
    list: (): Promise<SwarmNode[]> => request<SwarmNode[]>('/api/v1/nodes'),
    get: (id: string): Promise<SwarmNode> => request<SwarmNode>(`/api/v1/nodes/${id}`),
    update: (id: string, req: UpdateNodeRequest): Promise<SwarmNode> =>
      request<SwarmNode>(`/api/v1/nodes/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/nodes/${id}`, { method: 'DELETE' }),
    getJoinTokens: (): Promise<JoinTokensResponse> =>
      request<JoinTokensResponse>('/api/v1/nodes/join-tokens'),
  },

  // --- Managed Machines & Infrastructure ---
  machines: {
    list: (): Promise<MachineDTO[]> => request<MachineDTO[]>('/api/v1/machines'),
    get: (id: string): Promise<MachineDTO> => request<MachineDTO>(`/api/v1/machines/${id}`),
    delete: (id: string): Promise<{ message: string; id: string }> =>
      request(`/api/v1/machines/${id}`, { method: 'DELETE' }),
    getMetrics: (id: string): Promise<HostMetrics> =>
      request<HostMetrics>(`/api/v1/machines/${id}/metrics`),
    getEnrollCommand: (serverUrl?: string): Promise<EnrollMachineResponse> => {
      const qs = serverUrl ? `?server_url=${encodeURIComponent(serverUrl)}` : '';
      return request<EnrollMachineResponse>(`/api/v1/machines/enroll${qs}`);
    },
    joinSwarm: (id: string, req: JoinSwarmRequest): Promise<SwarmNode> =>
      request<SwarmNode>(`/api/v1/machines/${id}/join-swarm`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Databases ---
  databases: {
    list: (): Promise<Database[]> => request<Database[]>('/api/v1/databases'),
    get: (id: string): Promise<Database> => request<Database>(`/api/v1/databases/${id}`),
    create: (req: CreateDatabaseRequest): Promise<Database> =>
      request<Database>('/api/v1/databases', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: UpdateDatabaseRequest): Promise<Database> =>
      request<Database>(`/api/v1/databases/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(req),
      }),
    restart: (id: string): Promise<{ status: string }> =>
      request(`/api/v1/databases/${id}/restart`, { method: 'POST' }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/databases/${id}`, { method: 'DELETE' }),
  },

  // --- Backups ---
  backups: {
    list: (): Promise<Backup[]> => request<Backup[]>('/api/v1/backups'),
    create: (serviceId: string): Promise<Backup> =>
      request<Backup>('/api/v1/backups', {
        method: 'POST',
        body: JSON.stringify({ service_id: serviceId }),
      }),
    get: (id: string): Promise<Backup> => request<Backup>(`/api/v1/backups/${id}`),
    restore: (id: string, targetServiceId?: string): Promise<{ status: string }> =>
      request(`/api/v1/backups/${id}/restore`, {
        method: 'POST',
        body: JSON.stringify({ target_service_id: targetServiceId }),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/backups/${id}`, { method: 'DELETE' }),
    listDestinations: (): Promise<BackupDestination[]> =>
      request<BackupDestination[]>('/api/v1/backups/destinations'),
    createDestination: (dest: Partial<BackupDestination>): Promise<BackupDestination> =>
      request<BackupDestination>('/api/v1/backups/destinations', {
        method: 'POST',
        body: JSON.stringify(dest),
      }),
  },

  // --- Ingress ---
  ingress: {
    listDomains: (): Promise<DomainBinding[]> =>
      request<DomainBinding[]>('/api/v1/ingress/domains'),
    bindDomain: (req: BindDomainRequest): Promise<DomainBinding> =>
      request<DomainBinding>('/api/v1/ingress/domains', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    deleteDomain: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/ingress/domains/${id}`, { method: 'DELETE' }),
    uploadCertificate: (req: CertificateUploadRequest): Promise<{ status: string }> =>
      request('/api/v1/ingress/certificates', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    reconcile: (): Promise<{ status: string }> =>
      request('/api/v1/ingress/reconcile', { method: 'POST' }),
    getCaddyConfig: (): Promise<CaddyDiagnosticsDTO> =>
      request<CaddyDiagnosticsDTO>('/api/v1/ingress/caddy/config'),
  },

  // --- Registry ---
  registry: {
    getStatus: (): Promise<RegistryStatusResponse> =>
      request<RegistryStatusResponse>('/api/v1/registry/status'),
    listRepositories: (): Promise<RepositoryCatalogResponse> =>
      request<RepositoryCatalogResponse>('/api/v1/registry/repositories'),
    getCredentials: (projectId?: string): Promise<RobotCredentialsResponse[]> =>
      request<RobotCredentialsResponse[]>(
        `/api/v1/registry/credentials${projectId ? `?project_id=${projectId}` : ''}`
      ),
    rotateCredentials: (id: string): Promise<RobotCredentialsResponse> =>
      request<RobotCredentialsResponse>(`/api/v1/registry/credentials/rotate?id=${id}`, {
        method: 'POST',
      }),
    garbageCollect: (): Promise<{ status: string }> =>
      request('/api/v1/registry/garbage-collect', { method: 'POST' }),
  },

  // --- System ---
  system: {
    getInfo: (): Promise<SystemInfo> => request<SystemInfo>('/api/v1/system/info'),
    getDiskUsage: (): Promise<DiskUsageInfo> =>
      request<DiskUsageInfo>('/api/v1/system/disk'),
    prune: (req: PruneRequest): Promise<PruneResult> =>
      request<PruneResult>('/api/v1/system/prune', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Builds ---
  builds: {
    list: (appId: string): Promise<Build[]> =>
      request<Build[]>(`/api/v1/apps/${appId}/builds`),
    get: (buildId: string): Promise<Build> =>
      request<Build>(`/api/v1/builds/${buildId}`),
    rebuild: (buildId: string): Promise<Build> =>
      request<Build>(`/api/v1/builds/${buildId}/rebuild`, {
        method: 'POST',
      }),
    cancel: (buildId: string): Promise<void> =>
      request<void>(`/api/v1/builds/${buildId}/cancel`, {
        method: 'POST',
      }),
    streamUrl: (buildId: string): string => `/api/v1/builds/${buildId}/stream`,
  },

  // --- Templates & Marketplace ---
  templates: {
    list: (category?: string, search?: string): Promise<Template[]> => {
      const params = new URLSearchParams();
      if (category && category.toLowerCase() !== 'all') {
        params.set('category', category);
      }
      if (search && search.trim() !== '') {
        params.set('search', search.trim());
      }
      const qs = params.toString();
      return request<Template[]>(`/api/v1/templates${qs ? `?${qs}` : ''}`);
    },
    get: (id: string): Promise<Template> =>
      request<Template>(`/api/v1/templates/${id}`),
    deploy: (id: string, req: DeployTemplateRequest): Promise<DeployTemplateResponse> =>
      request<DeployTemplateResponse>(`/api/v1/templates/${id}/deploy`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Backup Schedules ---
  schedules: {
    list: (serviceId?: string): Promise<BackupSchedule[]> =>
      request<BackupSchedule[]>(
        `/api/v1/backups/schedules${serviceId ? `?service_id=${serviceId}` : ''}`
      ),
    create: (req: CreateBackupScheduleRequest): Promise<BackupSchedule> =>
      request<BackupSchedule>('/api/v1/backups/schedules', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: UpdateBackupScheduleRequest): Promise<BackupSchedule> =>
      request<BackupSchedule>(`/api/v1/backups/schedules/${id}`, {
        method: 'PATCH',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/backups/schedules/${id}`, { method: 'DELETE' }),
  },

  // --- Notification Channels ---
  notifications: {
    list: (projectId?: string): Promise<NotificationChannel[]> =>
      request<NotificationChannel[]>(
        `/api/v1/notifications/channels${projectId ? `?project_id=${projectId}` : ''}`
      ),
    create: (req: CreateNotificationChannelRequest): Promise<NotificationChannel> =>
      request<NotificationChannel>('/api/v1/notifications/channels', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: Partial<CreateNotificationChannelRequest>): Promise<NotificationChannel> =>
      request<NotificationChannel>(`/api/v1/notifications/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/notifications/channels/${id}`, { method: 'DELETE' }),
    test: (id: string): Promise<{ status: string; message: string }> =>
      request<{ status: string; message: string }>(`/api/v1/notifications/channels/${id}/test`, {
        method: 'POST',
      }),
  },

  // --- Team & User Management ---
  users: {
    list: (): Promise<TeamUser[]> => request<TeamUser[]>('/api/v1/users'),
    invite: (req: InviteUserRequest): Promise<TeamInvitationResponse> =>
      request<TeamInvitationResponse>('/api/v1/users/invite', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    acceptInvite: (req: AcceptInviteRequest): Promise<LoginResponse> =>
      request<LoginResponse>('/api/v1/users/accept-invite', {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    updateRole: (id: string, req: UpdateUserRoleRequest): Promise<{ message: string }> =>
      request<{ message: string }>(`/api/v1/users/${id}/role`, {
        method: 'PUT',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request<{ message: string }>(`/api/v1/users/${id}`, { method: 'DELETE' }),
    resetPassword: (id: string, req: ResetPasswordRequest): Promise<{ message: string }> =>
      request<{ message: string }>(`/api/v1/users/${id}/reset-password`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
  },

  // --- Project Memberships ---
  projectMembers: {
    list: (projectId: string): Promise<ProjectMember[]> =>
      request<ProjectMember[]>(`/api/v1/projects/${projectId}/members`),
    set: (projectId: string, req: SetProjectMemberRequest): Promise<ProjectMember> =>
      request<ProjectMember>(`/api/v1/projects/${projectId}/members`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    remove: (projectId: string, userId: string): Promise<{ message: string }> =>
      request<{ message: string }>(`/api/v1/projects/${projectId}/members/${userId}`, {
        method: 'DELETE',
      }),
  },

  // --- Developer Integrations Hub ---
  integrations: {
    list: (orgId?: string): Promise<Integration[]> =>
      request<Integration[]>(`/api/v1/integrations${orgId ? `?org_id=${orgId}` : ''}`),
    get: (id: string): Promise<Integration> => request<Integration>(`/api/v1/integrations/${id}`),
    create: (req: CreateIntegrationRequest, orgId?: string): Promise<Integration> =>
      request<Integration>(`/api/v1/integrations${orgId ? `?org_id=${orgId}` : ''}`, {
        method: 'POST',
        body: JSON.stringify(req),
      }),
    update: (id: string, req: UpdateIntegrationRequest): Promise<Integration> =>
      request<Integration>(`/api/v1/integrations/${id}`, {
        method: 'PUT',
        body: JSON.stringify(req),
      }),
    delete: (id: string): Promise<{ message: string }> =>
      request<{ message: string }>(`/api/v1/integrations/${id}`, { method: 'DELETE' }),
    test: (id: string): Promise<TestIntegrationResponse> =>
      request<TestIntegrationResponse>(`/api/v1/integrations/${id}/test`, {
        method: 'POST',
      }),
  },
};
