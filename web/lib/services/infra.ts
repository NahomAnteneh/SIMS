const INFRA_API_URL = process.env.INTERNAL_INFRA_API_URL || 'http://localhost:3001';

async function infraFetch<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(`${INFRA_API_URL}${endpoint}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      // 'Authorization': `Bearer ${process.env.INTERNAL_API_TOKEN}`, // Add if your 3001 backend requires auth
      ...options.headers,
    },
    // Next.js Cache Strategy: Revalidate data every 60 seconds
    next: { revalidate: 60 }, 
  });

  if (!response.ok) {
    throw new Error(`Infra API Error: ${response.status} on ${endpoint}`);
  }

  return response.json() as Promise<T>;
}

export const InfraService = {
  // Core Infrastructure
  getDatacenters:    () => infraFetch<any[]>('/datacenters'),
  getDatacenterById: (id: string) => infraFetch<any>(`/datacenters/${id}`),

  getVcenters:       () => infraFetch<any[]>('/vcenters'),
  getVcenterById:    (id: string) => infraFetch<any>(`/vcenters/${id}`),

  getClusters:       () => infraFetch<any[]>('/clusters'),
  getClusterById:    (id: string) => infraFetch<any>(`/clusters/${id}`),

  getHosts:          () => infraFetch<any[]>('/hosts'),
  getHostById:       (id: string) => infraFetch<any>(`/hosts/${id}`),

  // Business & Workloads
  getClients:        () => infraFetch<any[]>('/clients'),
  getClientById:     (id: string) => infraFetch<any>(`/clients/${id}`),

  getApplications:   () => infraFetch<any[]>('/applications'),
  getApplicationById:(id: string) => infraFetch<any>(`/applications/${id}`),

  getVms:            () => infraFetch<any[]>('/vms'),
  getVmById:         (id: string) => infraFetch<any>(`/vms/${id}`),

  // Storage & Networking (Added)
  getDatastores:     () => infraFetch<any[]>('/datastores'),
  getDatastoreById:  (id: string) => infraFetch<any>(`/datastores/${id}`),

  getNetworks:       () => infraFetch<any[]>('/networks'),
  getNetworkById:    (id: string) => infraFetch<any>(`/networks/${id}`),

  // Data Sources & Monitoring (Added)
  getDataSources:    () => infraFetch<any[]>('/dataSources'),
  getDataSourceById: (id: string) => infraFetch<any>(`/dataSources/${id}`),

  getActivities:     () => infraFetch<any[]>('/activities'),
  getActivityById:   (id: string) => infraFetch<any>(`/activities/${id}`),
};