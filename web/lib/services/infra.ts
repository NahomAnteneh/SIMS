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
  getDatacenters:  () => infraFetch<any[]>('/datacenters'),
  getVcenters:     () => infraFetch<any[]>('/vcenters'),
  getClusters:     () => infraFetch<any[]>('/clusters'),
  getHosts:        () => infraFetch<any[]>('/hosts'),
  getClients:      () => infraFetch<any[]>('/clients'),
  getApplications: () => infraFetch<any[]>('/applications'),
  getVms:          () => infraFetch<any[]>('/vms'),
  
  // Example of a granular or dynamic call if needed later
  getVmById:       (id: string) => infraFetch<any>(`/vms/${id}`),
};