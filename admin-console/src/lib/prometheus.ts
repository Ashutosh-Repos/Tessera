// Prometheus HTTP API client for the SRE Admin Console

export interface MetricResult {
  metric: Record<string, string>;
  value: [number, string]; // [timestamp, value]
}

export interface RangeResult {
  metric: Record<string, string>;
  values: [number, string][]; // [[timestamp, value], ...]
}

interface PrometheusResponse<T> {
  status: 'success' | 'error';
  data: {
    resultType: string;
    result: T[];
  };
}

export class PrometheusClient {
  private baseUrl: string;

  constructor(baseUrl: string = 'http://localhost:9091') {
    this.baseUrl = baseUrl.replace(/\/$/, '');
  }

  async queryInstant(query: string): Promise<MetricResult[]> {
    try {
      const url = `${this.baseUrl}/api/v1/query?query=${encodeURIComponent(query)}`;
      const res = await fetch(url);
      if (!res.ok) return [];
      const data: PrometheusResponse<MetricResult> = await res.json();
      return data.data.result;
    } catch {
      return [];
    }
  }

  async queryRange(
    query: string,
    start: number,
    end: number,
    step: string = '15s'
  ): Promise<RangeResult[]> {
    try {
      const url = `${this.baseUrl}/api/v1/query_range?query=${encodeURIComponent(query)}&start=${start}&end=${end}&step=${step}`;
      const res = await fetch(url);
      if (!res.ok) return [];
      const data: PrometheusResponse<RangeResult> = await res.json();
      return data.data.result;
    } catch {
      return [];
    }
  }

  async getMetricValue(query: string): Promise<number> {
    const results = await this.queryInstant(query);
    if (results.length === 0) return 0;
    return parseFloat(results[0].value[1]) || 0;
  }
}

export const prometheus = new PrometheusClient();
