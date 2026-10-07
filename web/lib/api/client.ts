import {
  LoginRequest,
  LoginResponse,
  UserResponse,
  UserListResponse,
  UpdateUserRequest,
  ListVMsResponse,
  VMDetailResponse,
  SearchVMsRequest,
  SearchVMsResponse,
  VMFacetsResponse,
  TagMetadataItem,
  TagMetadataListResponse,
  CustomAttributeMetadataItem,
  CustomAttributeMetadataListResponse,
  VMFieldMeta,
  VMFieldsMetaResponse,
  TopologyItem,
  TopologyListResponse,
  FolderItem,
  FolderListResponse,
  VCenterResponse,
  VCenterListResponse,
  AuditEventItem,
  AuditEventListResponse,
  ProblemDetails,
} from "./types";

const TOKEN_KEY = "sims_auth_token";

export class ApiError extends Error {
  status: number;
  problem?: ProblemDetails;

  constructor(status: number, message: string, problem?: ProblemDetails) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }
}

function getBaseUrl(): string {
  if (typeof window !== "undefined") {
    // In browser, prefer NEXT_PUBLIC_QUERY_API_URL or default to same-origin /api proxy
    return process.env.NEXT_PUBLIC_QUERY_API_URL || "/api";
  }
  // Server-side requires absolute URL
  return (
    process.env.INTERNAL_QUERY_API_URL ||
    process.env.QUERY_API_URL ||
    process.env.NEXT_PUBLIC_QUERY_API_URL ||
    "http://localhost:8080"
  );
}

export function getAuthToken(): string | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    // Check localStorage
    const local = localStorage.getItem(TOKEN_KEY);
    if (local) return local;

    // Check document.cookie
    const match = document.cookie.match(new RegExp("(^| )" + TOKEN_KEY + "=([^;]+)"));
    if (match) return decodeURIComponent(match[2]);
  } catch {
    // ignore
  }
  return null;
}

export function setAuthToken(token: string | null): void {
  if (typeof window === "undefined") return;
  try {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token);
      document.cookie = `${TOKEN_KEY}=${encodeURIComponent(token)}; path=/; max-age=86400; SameSite=Lax`;
    } else {
      localStorage.removeItem(TOKEN_KEY);
      document.cookie = `${TOKEN_KEY}=; path=/; max-age=0; SameSite=Lax`;
    }
  } catch {
    // ignore
  }
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {},
  customToken?: string
): Promise<T> {
  const baseUrl = getBaseUrl();
  const url = `${baseUrl}${endpoint}`;

  const token = customToken || getAuthToken();

  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(options.headers as Record<string, string>),
  };

  if (options.body && typeof options.body === "string" && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }

  if (token && !headers["Authorization"]) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  try {
    const res = await fetch(url, {
      ...options,
      headers,
    });

    if (!res.ok) {
      let problem: ProblemDetails | undefined;
      let errMsg = `Request failed with status ${res.status}`;
      try {
        const body = await res.json();
        problem = body;
        errMsg = body.detail || body.message || body.title || errMsg;
      } catch {
        // non-json error body
      }
      throw new ApiError(res.status, errMsg, problem);
    }

    if (res.status === 204) {
      return {} as T;
    }

    return (await res.json()) as T;
  } catch (err: any) {
    if (err instanceof ApiError) {
      throw err;
    }
    throw new ApiError(0, err?.message || "Network request failed");
  }
}

export const simsApi = {
  // Authentication & Users
  async login(credentials: LoginRequest): Promise<LoginResponse> {
    const res = await request<LoginResponse>("/v1/auth/login", {
      method: "POST",
      body: JSON.stringify(credentials),
    });
    if (res.token) {
      setAuthToken(res.token);
    }
    return res;
  },

  logout(): void {
    setAuthToken(null);
  },

  async getMe(token?: string): Promise<UserResponse> {
    return request<UserResponse>("/v1/me", {}, token);
  },

  async listUsers(): Promise<UserResponse[]> {
    const res = await request<UserListResponse>("/v1/users");
    return res.items || [];
  },

  async createUser(payload: {
    username: string;
    password: string;
    email?: string;
    role: string;
    allowed_vcenter_ids?: string[];
    allowed_folder_paths?: string[];
  }): Promise<UserResponse> {
    return request<UserResponse>("/v1/users", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  async getUser(id: string): Promise<UserResponse> {
    return request<UserResponse>(`/v1/users/${id}`);
  },

  async updateUser(id: string, payload: UpdateUserRequest): Promise<UserResponse> {
    return request<UserResponse>(`/v1/users/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },

  async deleteUser(id: string): Promise<void> {
    return request<void>(`/v1/users/${id}`, {
      method: "DELETE",
    });
  },

  // Virtual Machines
  async listVMs(params?: {
    limit?: number;
    cursor?: string;
    name?: string;
    vcenter_id?: string;
  }): Promise<ListVMsResponse> {
    const sp = new URLSearchParams();
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.cursor) sp.set("cursor", params.cursor);
    if (params?.name) sp.set("name", params.name);
    if (params?.vcenter_id) sp.set("vcenter_id", params.vcenter_id);

    const qs = sp.toString();
    return request<ListVMsResponse>(`/v1/vms${qs ? `?${qs}` : ""}`);
  },

  async getVM(id: string): Promise<VMDetailResponse> {
    return request<VMDetailResponse>(`/v1/vms/${id}`);
  },

  async quickSearchVMs(q: string, limit?: number, cursor?: string): Promise<SearchVMsResponse> {
    const sp = new URLSearchParams({ q });
    if (limit) sp.set("limit", limit.toString());
    if (cursor) sp.set("cursor", cursor);
    return request<SearchVMsResponse>(`/v1/search/vms?${sp.toString()}`);
  },

  async searchVMs(payload: SearchVMsRequest): Promise<SearchVMsResponse> {
    return request<SearchVMsResponse>("/v1/vms/search", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  async getVMFacets(fields?: string, limit?: number): Promise<VMFacetsResponse> {
    const sp = new URLSearchParams();
    if (fields) sp.set("fields", fields);
    if (limit) sp.set("limit", limit.toString());
    const qs = sp.toString();
    return request<VMFacetsResponse>(`/v1/vms/facets${qs ? `?${qs}` : ""}`);
  },

  async listTags(): Promise<TagMetadataItem[]> {
    const res = await request<TagMetadataListResponse>("/v1/tags");
    return res.items || [];
  },

  async listCustomAttributes(): Promise<CustomAttributeMetadataItem[]> {
    const res = await request<CustomAttributeMetadataListResponse>("/v1/custom-attributes");
    return res.items || [];
  },

  async getVMFields(): Promise<VMFieldMeta[]> {
    const res = await request<VMFieldsMetaResponse>("/v1/meta/vm-fields");
    return res.fields || [];
  },

  // Infrastructure Topology
  async listDatacenters(): Promise<TopologyItem[]> {
    const res = await request<TopologyListResponse>("/v1/datacenters");
    return res.items || [];
  },

  async getDatacenter(id: string): Promise<TopologyItem> {
    return request<TopologyItem>(`/v1/datacenters/${id}`);
  },

  async listClusters(): Promise<TopologyItem[]> {
    const res = await request<TopologyListResponse>("/v1/clusters");
    return res.items || [];
  },

  async getCluster(id: string): Promise<TopologyItem> {
    return request<TopologyItem>(`/v1/clusters/${id}`);
  },

  async listHosts(): Promise<TopologyItem[]> {
    const res = await request<TopologyListResponse>("/v1/hosts");
    return res.items || [];
  },

  async getHost(id: string): Promise<TopologyItem> {
    return request<TopologyItem>(`/v1/hosts/${id}`);
  },

  async listDatastores(): Promise<TopologyItem[]> {
    const res = await request<TopologyListResponse>("/v1/datastores");
    return res.items || [];
  },

  async getDatastore(id: string): Promise<TopologyItem> {
    return request<TopologyItem>(`/v1/datastores/${id}`);
  },

  async listNetworks(): Promise<TopologyItem[]> {
    const res = await request<TopologyListResponse>("/v1/networks");
    return res.items || [];
  },

  async getNetwork(id: string): Promise<TopologyItem> {
    return request<TopologyItem>(`/v1/networks/${id}`);
  },

  async listFolders(): Promise<FolderItem[]> {
    const res = await request<FolderListResponse>("/v1/folders");
    return res.items || [];
  },

  async getFolder(id: string): Promise<FolderItem> {
    return request<FolderItem>(`/v1/folders/${id}`);
  },

  async listTopologyVMs(
    type: "datacenter" | "cluster" | "host" | "datastore" | "network" | "folder",
    id: string,
    params?: { limit?: number; cursor?: string }
  ): Promise<ListVMsResponse> {
    const plural = type === "folder" ? "folders" : `${type}s`;
    const sp = new URLSearchParams();
    if (params?.limit) sp.set("limit", params.limit.toString());
    if (params?.cursor) sp.set("cursor", params.cursor);
    const qs = sp.toString();
    return request<ListVMsResponse>(`/v1/${plural}/${id}/vms${qs ? `?${qs}` : ""}`);
  },

  // vCenter Management
  async listVCenters(): Promise<VCenterResponse[]> {
    const res = await request<VCenterListResponse>("/v1/vcenters");
    return res.items || [];
  },

  async getVCenter(id: string): Promise<VCenterResponse> {
    return request<VCenterResponse>(`/v1/vcenters/${id}`);
  },

  async testVCenterConnection(payload: {
    host: string;
    port?: number;
    username: string;
    password: string;
    insecure_tls?: boolean;
    tls?: { verify: boolean; ca_pem?: string };
  }): Promise<{ status: string; version?: any; capabilities?: any }> {
    const port = payload.port || 443;
    const body = {
      host: payload.host,
      port,
      username: payload.username,
      password: payload.password,
      tls: payload.tls || { verify: !payload.insecure_tls },
    };
    return request<{ status: string; version?: any; capabilities?: any }>(
      "/v1/vcenters/test-connection",
      {
        method: "POST",
        body: JSON.stringify(body),
      }
    );
  },

  async createVCenter(payload: {
    name: string;
    host: string;
    port?: number;
    username: string;
    password: string;
    insecure_tls?: boolean;
    tls?: { verify: boolean; ca_pem?: string };
    enabled?: boolean;
    priority?: number;
  }): Promise<VCenterResponse> {
    const port = payload.port || 443;
    const body = {
      name: payload.name,
      host: payload.host,
      port,
      username: payload.username,
      password: payload.password,
      tls: payload.tls || { verify: !payload.insecure_tls },
      enabled: payload.enabled ?? true,
      priority: payload.priority ?? 1,
    };
    return request<VCenterResponse>("/v1/vcenters", {
      method: "POST",
      body: JSON.stringify(body),
    });
  },

  async updateVCenter(
    id: string,
    payload: {
      name?: string;
      username?: string;
      password?: string;
      enabled?: boolean;
      priority?: number;
    }
  ): Promise<VCenterResponse> {
    return request<VCenterResponse>(`/v1/vcenters/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },

  async deleteVCenter(id: string): Promise<void> {
    return request<void>(`/v1/vcenters/${id}`, {
      method: "DELETE",
    });
  },

  async resyncVCenter(id: string): Promise<{ message: string }> {
    return request<{ message: string }>(`/v1/vcenters/${id}/resync`, {
      method: "POST",
    });
  },

  async listAuditEvents(params?: { limit?: number }): Promise<AuditEventItem[]> {
    const sp = new URLSearchParams();
    if (params?.limit) sp.set("limit", params.limit.toString());
    const qs = sp.toString();
    const res = await request<AuditEventListResponse>(`/v1/audit-events${qs ? `?${qs}` : ""}`);
    return res.items || [];
  },
};
