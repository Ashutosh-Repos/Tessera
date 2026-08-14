// Gateway API client for the SRE Admin Console

export interface JobStatus {
  job_id: string;
  phase: 'CREATED' | 'SLICING' | 'TRANSCODING' | 'COMPILING' | 'COMPLETED' | 'FAILED';
  completed: number;
  total: number;
  owner_epoch: number;
  partition_id: number;
  last_updated: number;
}

export interface RegionHealth {
  region: string;
  gateway_url: string;
  healthy: boolean;
  services: {
    redis: boolean;
    nats: boolean;
    s3: boolean;
    etcd: boolean;
  };
  active_sockets: number;
  upload_count: number;
  dlq_depth: number;
  workers: Array<{ id: string; cpu: number; gpu: number; tasks: number }>;
}

export class GatewayClient {
  private baseUrl: string;
  private apiKey?: string;

  constructor(baseUrl: string, apiKey?: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.apiKey = apiKey;
  }

  setApiKey(key: string) {
    this.apiKey = key;
  }

  private getHeaders(): HeadersInit {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    // Prioritize explicit instance key or user-bound session token
    const token = this.apiKey || (typeof window !== 'undefined' ? (sessionStorage.getItem('tessera_admin_token') || localStorage.getItem('tessera_admin_token') || undefined) : undefined);
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
    return headers;
  }

  async getHealth(): Promise<boolean> {
    try {
      const res = await fetch(`${this.baseUrl}/health`);
      return res.ok;
    } catch {
      return false;
    }
  }

  async getJobStatus(uuid: string): Promise<JobStatus | null> {
    try {
      const res = await fetch(`${this.baseUrl}/api/jobs/${uuid}/status`);
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  }

  async listJobs(): Promise<JobStatus[]> {
    try {
      const res = await fetch(`${this.baseUrl}/api/admin/jobs`, {
        headers: this.getHeaders(),
      });
      if (!res.ok) return [];
      return await res.json();
    } catch {
      return [];
    }
  }

  async getRegionHealth(): Promise<RegionHealth | null> {
    try {
      const res = await fetch(`${this.baseUrl}/api/admin/regions`, {
        headers: this.getHeaders(),
      });
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  }

  async listCoordinators(): Promise<string[]> {
    try {
      const res = await fetch(`${this.baseUrl}/api/admin/coordinators`, {
        headers: this.getHeaders(),
      });
      if (!res.ok) return [];
      return await res.json();
    } catch {
      return [];
    }
  }
}

// Pre-configured clients for known regions with regional gateway endpoints
const usEastUrl = (import.meta as unknown as { env?: Record<string, string> }).env?.VITE_API_BASE_URL || 'http://localhost:8080';
const euWestUrl = (import.meta as unknown as { env?: Record<string, string> }).env?.VITE_EU_API_BASE_URL || 'http://localhost:8090';

export const regions: Record<string, GatewayClient> = {
  'us-east': new GatewayClient(usEastUrl),
  'eu-west': new GatewayClient(euWestUrl),
};
