// Type definitions for SIMS Query API (v1.0.0)

export interface UserResponse {
  id: string;
  username: string;
  email?: string;
  role: "admin" | "user" | string;
  allowed_vcenter_ids?: string[];
  allowed_folder_paths?: string[];
  created_at: string;
  updated_at: string;
}

export interface UserListResponse {
  items: UserResponse[];
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  token_type: string;
  expires_at: string;
  user: UserResponse;
}

export interface VMListItem {
  id: string;
  vcenter_id: string;
  moref: string;
  instance_uuid: string;
  bios_uuid?: string;
  name: string;
  power_state: "poweredOn" | "poweredOff" | "suspended" | string;
  connection_state: "connected" | "disconnected" | string;
  cpu_cores: number;
  memory_mb: number;
  folder_id?: string;
  folder_path?: string;
  host_id?: string;
  cluster_id?: string;
  datacenter_id?: string;
  guest_os?: string;
  guest_host_name?: string;
  guest_ip_address?: string;
  annotation?: string;
  created_at: string;
  updated_at: string;
  first_seen_at?: string;
}

export interface PaginationMeta {
  limit: number;
  next_cursor?: string;
  has_more: boolean;
}

export interface ListVMsResponse {
  items: VMListItem[];
  pagination: PaginationMeta;
}

export interface DatastoreAttachment {
  id: string;
  moref: string;
  name: string;
  type: string;
  capacity_bytes: number;
  free_bytes: number;
}

export interface NICAttachment {
  id: string;
  device_key: number;
  mac_address: string;
  ip_address?: string;
  network_id?: string;
  portgroup_id?: string;
}

export interface TagItem {
  tag_id: string;
  category_name: string;
  tag_name: string;
}

export interface CustomAttributeItem {
  name: string;
  value: string;
}

export interface VMDetailResponse extends VMListItem {
  datastores: DatastoreAttachment[];
  network_interfaces: NICAttachment[];
  tags: TagItem[];
  custom_attributes: CustomAttributeItem[];
}

export interface SearchFilter {
  op: string;
  field?: string;
  value?: any;
  values?: any[];
  operands?: SearchFilter[];
  operand?: SearchFilter;
  category?: string;
  tag?: string;
  name?: string;
}

export interface SearchVMsRequest {
  filter?: SearchFilter;
  sort?: { field: string; direction: "asc" | "desc" };
  limit?: number;
  cursor?: string;
}

export interface SearchVMsResponse {
  items: VMListItem[];
  nextCursor?: string;
  hasMore: boolean;
}

export interface FacetBucket {
  value: string;
  count: number;
}

export interface VMFacetsResponse {
  facets: Record<string, FacetBucket[]>;
}

export interface TagMetadataItem {
  category_name: string;
  tag_name: string;
  vm_count: number;
}

export interface TagMetadataListResponse {
  items: TagMetadataItem[];
}

export interface CustomAttributeMetadataItem {
  name: string;
  vm_count: number;
  sample_values?: string[];
}

export interface CustomAttributeMetadataListResponse {
  items: CustomAttributeMetadataItem[];
}

export interface VMFieldMeta {
  name: string;
  type: string;
  allowed_operators: string[];
  description: string;
}

export interface VMFieldsMetaResponse {
  fields: VMFieldMeta[];
}

export interface UpdateUserRequest {
  email?: string;
  role?: string;
  password?: string;
  allowed_vcenter_ids?: string[];
  allowed_folder_paths?: string[];
}

export interface TopologyItem {
  id: string;
  vcenter_id: string;
  name: string;
  moref?: string;
  datacenter_id?: string;
  cluster_id?: string;
  connection_state?: string;
  power_state?: string;
  capacity_bytes?: number;
  free_bytes?: number;
  type?: string;
}

export interface TopologyListResponse {
  items: TopologyItem[];
}

export interface FolderItem {
  id: string;
  vcenter_id: string;
  name: string;
  path: string;
  vm_count: number;
}

export interface FolderListResponse {
  items: FolderItem[];
}

export interface VCenterVersion {
  version: string;
  build: string;
  product_version: string;
  api_version: string;
}

export interface VCenterResponse {
  id: string;
  name: string;
  host: string;
  port: number;
  version: VCenterVersion;
  capabilities: Record<string, string>;
  enabled: boolean;
  status: string;
  priority: number;
  owning_worker_id?: string;
  last_sync_at?: string;
  created_at: string;
  updated_at: string;
}

export interface VCenterListResponse {
  items: VCenterResponse[];
}

export interface AuditEventItem {
  id: string;
  actor: string;
  action: string;
  target_id: string;
  outcome: string;
  request_id: string;
  details: Record<string, any>;
  created_at: string;
}

export interface AuditEventListResponse {
  items: AuditEventItem[];
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  detail: string;
  code: number;
  error_code?: string;
  error?: string;
  message?: string;
}
