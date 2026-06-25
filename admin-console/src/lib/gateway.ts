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

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
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
      const res = await fetch(`${this.baseUrl}/api/admin/jobs`);
      if (!res.ok) return [];
      return await res.json();
    } catch {
      return [];
    }
  }

  async getRegionHealth(): Promise<RegionHealth | null> {
    try {
      const res = await fetch(`${this.baseUrl}/api/admin/regions`);
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  }

  async listCoordinators(): Promise<string[]> {
    try {
      const res = await fetch(`${this.baseUrl}/api/admin/coordinators`);
      if (!res.ok) return [];
      return await res.json();
    } catch {
      return [];
    }
  }
}

// Pre-configured clients for known regions
export const regions = {
  'us-east': new GatewayClient('http://localhost:8080'),
  'eu-west': new GatewayClient('http://localhost:8090'),
};
