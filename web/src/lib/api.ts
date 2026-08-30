import {
  ApiResponse,
  APIError,
  App,
  CreateAppRequest,
  UpdateAppRequest,
  DeployAppRequest,
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

  return (json as ApiResponse<T>).data;
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

  // --- Apps ---
  apps: {
    list: (): Promise<App[]> => request<App[]>('/api/v1/apps'),
    get: (id: string): Promise<App> => request<App>(`/api/v1/apps/${id}`),
    create: (req: CreateAppRequest): Promise<App> =>
      request<App>('/api/v1/apps', {
        method: 'POST',
        body: JSON.stringify(req),
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
    delete: (id: string): Promise<{ message: string }> =>
      request(`/api/v1/stacks/${id}`, { method: 'DELETE' }),
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
};
