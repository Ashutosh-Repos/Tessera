import React, { useEffect, useState, useRef } from 'react';
import { MetricCard } from '../components/MetricCard';
import { RegionCard } from '../components/RegionCard';
import { regions } from '../lib/gateway';
import { 
  Server, 
  UploadCloud, 
  CheckCircle2, 
  AlertTriangle, 
  Wifi, 
  Cpu 
} from 'lucide-react';

export const Dashboard: React.FC = () => {
  const [activeJobs, setActiveJobs] = useState<number>(0);
  const [uploadRate, setUploadRate] = useState<number>(0.0);
  const [throughput, setThroughput] = useState<number>(0.0);
  const [dlqDepth, setDlqDepth] = useState<number>(0);
  const [activeSockets, setActiveSockets] = useState<number>(0);
  const [gpuUtil, setGpuUtil] = useState<number>(0);

  // Sparkline histories
  const [activeJobsHistory, setActiveJobsHistory] = useState<number[]>([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  const [uploadRateHistory, setUploadRateHistory] = useState<number[]>([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  const [throughputHistory, setThroughputHistory] = useState<number[]>([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  const [gpuHistory, setGpuHistory] = useState<number[]>([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);

  // Regions health status
  const [usEastHealth, setUsEastHealth] = useState({
    name: 'US East Region',
    code: 'us-east-1',
    gateways: 1,
    coordinators: 1,
    workers: 1,
    healthy: true,
    services: { redis: true, nats: true, s3: true, etcd: true },
    uploadsPerMin: 0.0,
    jobsActive: 0,
  });

  const [euWestHealth, setEuWestHealth] = useState({
    name: 'EU West Region',
    code: 'eu-west-1',
    gateways: 1,
    coordinators: 1,
    workers: 1,
    healthy: true,
    services: { redis: true, nats: true, s3: true, etcd: true },
    uploadsPerMin: 0.0,
    jobsActive: 0,
  });

  // Pipeline phases job counts
  const [pipelineJobs, setPipelineJobs] = useState({
    uploading: 0,
    slicing: 0,
    transcoding: 0,
    compiling: 0,
    completed: 0,
    failed: 0,
  });

  // Worker heatmap
  const [workers, setWorkers] = useState<Array<{ id: string; gpu: number; cpu: number; tasks: number }>>([]);

  const lastUsUploadRef = useRef<number>(0);
  const lastEuUploadRef = useRef<number>(0);
  const lastUploadCountRef = useRef<number>(0);
  const lastCompletedCountRef = useRef<number>(0);

  useEffect(() => {
    let active = true;

    const fetchData = async () => {
      // 1. Fetch coordinators list
      let usCoordsCount = 0;
      let euCoordsCount = 0;
      try {
        const usCoords = await regions['us-east'].listCoordinators();
        usCoordsCount = usCoords.length;
      } catch {}
      try {
        const euCoords = await regions['eu-west'].listCoordinators();
        euCoordsCount = euCoords.length;
      } catch {}

      // 2. Fetch region healths
      let usHealth: any = null;
      let euHealth: any = null;

      try {
        usHealth = await regions['us-east'].getRegionHealth();
        if (usHealth && active) {
          const usUploadDiff = usHealth.upload_count - lastUsUploadRef.current;
          const usUploadsPerMin = parseFloat((usUploadDiff * 15).toFixed(1));
          lastUsUploadRef.current = usHealth.upload_count;

          setUsEastHealth(prev => ({
            ...prev,
            healthy: usHealth.healthy,
            services: usHealth.services,
            coordinators: usCoordsCount,
            workers: usHealth.workers ? usHealth.workers.length : 0,
            gateways: usHealth.healthy ? 1 : 0,
            uploadsPerMin: usUploadsPerMin >= 0 ? usUploadsPerMin : 0,
          }));
        }
      } catch (err) {
        console.warn("Failed to fetch US East health:", err);
      }

      try {
        euHealth = await regions['eu-west'].getRegionHealth();
        if (euHealth && active) {
          const euUploadDiff = euHealth.upload_count - lastEuUploadRef.current;
          const euUploadsPerMin = parseFloat((euUploadDiff * 15).toFixed(1));
          lastEuUploadRef.current = euHealth.upload_count;

          setEuWestHealth(prev => ({
            ...prev,
            healthy: euHealth.healthy,
            services: euHealth.services,
            coordinators: euCoordsCount,
            workers: euHealth.workers ? euHealth.workers.length : 0,
            gateways: euHealth.healthy ? 1 : 0,
            uploadsPerMin: euUploadsPerMin >= 0 ? euUploadsPerMin : 0,
          }));
        }
      } catch (err) {
        console.warn("Failed to fetch EU West health:", err);
      }

      // 3. Fetch all jobs
      let allJobs: any[] = [];
      await Promise.all(
        Object.entries(regions).map(async ([regionName, client]) => {
          try {
            const regionalJobs = await client.listJobs();
            if (regionalJobs) {
              allJobs.push(...regionalJobs.map(j => ({ ...j, region: regionName })));
            }
          } catch (e) {
            console.warn(`Failed to fetch jobs from region ${regionName}:`, e);
          }
        })
      );

      if (!active) return;

      // 4. Update workers heatmap
      const mergedWorkers = [
        ...(usHealth?.workers || []),
        ...(euHealth?.workers || [])
      ];
      if (mergedWorkers.length > 0) {
        setWorkers(mergedWorkers);
      }

      // 5. Update DLQ Depth and Active Sockets
      const totalDlq = (usHealth?.dlq_depth || 0) + (euHealth?.dlq_depth || 0);
      const totalSockets = (usHealth?.active_sockets || 0) + (euHealth?.active_sockets || 0);
      setDlqDepth(totalDlq);
      setActiveSockets(totalSockets);

      // 6. Update Active Jobs & Pipeline Phases
      const activeJobsList = allJobs.filter(j => j.phase !== 'COMPLETED' && j.phase !== 'FAILED');
      const activeCount = activeJobsList.length;
      setActiveJobs(activeCount);
      setActiveJobsHistory(h => [...h.slice(1), activeCount]);

      const completedCount = allJobs.filter(j => j.phase === 'COMPLETED').length;
      const failedCount = allJobs.filter(j => j.phase === 'FAILED').length;

      const phases = { uploading: 0, slicing: 0, transcoding: 0, compiling: 0, completed: completedCount, failed: failedCount };
      allJobs.forEach(job => {
        if (job.phase === 'CREATED') phases.uploading++;
        else if (job.phase === 'SLICING') phases.slicing++;
        else if (job.phase === 'TRANSCODING') phases.transcoding++;
        else if (job.phase === 'COMPILING') phases.compiling++;
      });

      setPipelineJobs({
        uploading: phases.uploading || 0,
        slicing: phases.slicing || 0,
        transcoding: phases.transcoding || 0,
        compiling: phases.compiling || 0,
        completed: phases.completed,
        failed: phases.failed,
      });

      // 7. Calculate rates
      const currentUploadTotal = (usHealth?.upload_count || 0) + (euHealth?.upload_count || 0);
      const uploadDiff = currentUploadTotal - lastUploadCountRef.current;
      const rate = parseFloat((uploadDiff / 4).toFixed(2));
      lastUploadCountRef.current = currentUploadTotal;
      setUploadRate(rate >= 0 ? rate : 0);
      setUploadRateHistory(h => [...h.slice(1), rate >= 0 ? rate : 0]);

      const completedDiff = completedCount - lastCompletedCountRef.current;
      const tpRate = parseFloat((completedDiff * 15).toFixed(1)); // completed per minute
      lastCompletedCountRef.current = completedCount;
      setThroughput(tpRate >= 0 ? tpRate : 0);
      setThroughputHistory(h => [...h.slice(1), tpRate >= 0 ? tpRate : 0]);

      // 8. GPU utilization average
      if (mergedWorkers.length > 0) {
        const sumGpu = mergedWorkers.reduce((acc, w) => acc + w.gpu, 0);
        const avgGpu = Math.round(sumGpu / mergedWorkers.length);
        setGpuUtil(avgGpu);
        setGpuHistory(h => [...h.slice(1), avgGpu]);
      } else {
        setGpuUtil(0);
        setGpuHistory(h => [...h.slice(1), 0]);
      }

      // 9. Update jobs count on region objects
      const usActiveCount = allJobs.filter(j => j.region === 'us-east' && j.phase !== 'COMPLETED' && j.phase !== 'FAILED').length;
      const euActiveCount = allJobs.filter(j => j.region === 'eu-west' && j.phase !== 'COMPLETED' && j.phase !== 'FAILED').length;
      setUsEastHealth(prev => ({ ...prev, jobsActive: usActiveCount }));
      setEuWestHealth(prev => ({ ...prev, jobsActive: euActiveCount }));
    };

    fetchData();
    const interval = setInterval(fetchData, 4000);

    return () => {
      active = false;
      clearInterval(interval);
    };
  }, []);

  return (
    <div className="space-y-8 select-none">
      {/* Top Title Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-neutral-900 pb-6">
        <div>
          <h2 className="text-xl font-bold font-mono tracking-wider text-white uppercase">FLEET_DASHBOARD</h2>
          <p className="text-xs text-neutral-500 font-mono mt-1 uppercase tracking-wide">Multi-region distributed transcoding network status</p>
        </div>
        <div className="inline-flex items-center rounded bg-neutral-900 px-3 py-1 text-[10px] font-mono font-bold text-neutral-400 border border-neutral-800 tracking-widest uppercase">
          ● LIVE_MONITOR_ACTIVE
        </div>
      </div>

      {/* Fleet Telemetry Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <MetricCard
          title="Active Transcode Jobs"
          value={activeJobs}
          sparklineData={activeJobsHistory}
          trend="up"
          trendValue="8%"
          icon={<Server className="h-4 w-4" />}
        />
        <MetricCard
          title="Ingest rate"
          value={uploadRate}
          unit="req/s"
          sparklineData={uploadRateHistory}
          trend="up"
          trendValue="12%"
          icon={<UploadCloud className="h-4 w-4" />}
        />
        <MetricCard
          title="Completion rate"
          value={throughput}
          unit="jobs/m"
          sparklineData={throughputHistory}
          trend="neutral"
          trendValue="0%"
          icon={<CheckCircle2 className="h-4 w-4" />}
        />
        <MetricCard
          title="DLQ BACKLOG"
          value={dlqDepth}
          trend={dlqDepth > 0 ? 'up' : 'neutral'}
          trendValue={dlqDepth > 0 ? 'ALERT' : 'NOMINAL'}
          icon={<AlertTriangle className="h-4 w-4" />}
        />
        <MetricCard
          title="Live SSE Clients"
          value={activeSockets}
          unit="clients"
          trend="up"
          trendValue="4%"
          icon={<Wifi className="h-4 w-4" />}
        />
        <MetricCard
          title="Compute fleet load"
          value={`${gpuUtil}%`}
          sparklineData={gpuHistory}
          trend="neutral"
          trendValue="Avg"
          icon={<Cpu className="h-4 w-4" />}
        />
      </div>

      {/* Second Row: Maps and Pipeline */}
      <div className="grid gap-6 lg:grid-cols-2">
        
        {/* Region Map & Node Cards */}
        <div className="rounded border border-neutral-900 bg-black p-6 flex flex-col gap-6 shadow-sm">
          <h3 className="text-xs font-bold font-mono tracking-widest text-neutral-400 uppercase border-b border-neutral-900 pb-3">
            INFRASTRUCTURE DEPLOYMENT MAP
          </h3>
          
          {/* US-EU Map */}
          <div className="relative h-44 rounded border border-neutral-900 bg-neutral-950 overflow-hidden flex items-center justify-center">
            {/* SVG line */}
            <svg className="absolute inset-0 h-full w-full pointer-events-none">
              <line x1="30%" y1="45%" x2="70%" y2="45%" stroke="rgba(255, 255, 255, 0.2)" strokeWidth="1" strokeDasharray="3 3" />
            </svg>
            
            {/* US East Pin */}
            <div className="absolute left-[25%] top-[40%] flex flex-col items-center gap-1.5 z-10">
              <div className="relative flex h-3 w-3 items-center justify-center">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-white opacity-40"></span>
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-white"></span>
              </div>
              <span className="text-[9px] font-semibold font-mono text-neutral-400 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800 shadow-md">
                US-EAST-1
              </span>
            </div>

            {/* EU West Pin */}
            <div className="absolute right-[25%] top-[40%] flex flex-col items-center gap-1.5 z-10">
              <div className="relative flex h-3 w-3 items-center justify-center">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-white opacity-40"></span>
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-white"></span>
              </div>
              <span className="text-[9px] font-semibold font-mono text-neutral-400 bg-neutral-900 px-1.5 py-0.5 rounded border border-neutral-800 shadow-md">
                EU-WEST-1
              </span>
            </div>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <RegionCard {...usEastHealth} />
            <RegionCard {...euWestHealth} />
          </div>
        </div>

        {/* Job Pipeline & GPU Heatmap */}
        <div className="rounded border border-neutral-900 bg-black p-6 flex flex-col gap-6 shadow-sm">
          <h3 className="text-xs font-bold font-mono tracking-widest text-neutral-400 uppercase border-b border-neutral-900 pb-3">
            ACTIVE PIPELINE FLUX
          </h3>
          
          {/* Pipeline stages */}
          <div className="grid grid-cols-2 sm:grid-cols-5 gap-3 relative font-mono text-xs text-center">
            <div className="flex flex-col items-center p-3 rounded border border-neutral-900 bg-neutral-950/30 hover:border-neutral-800 transition-colors">
              <span className="text-sm font-bold text-neutral-300">{pipelineJobs.uploading}</span>
              <span className="text-[8px] text-neutral-500 uppercase mt-1">Ingestion</span>
            </div>
            
            <div className="flex flex-col items-center p-3 rounded border border-neutral-900 bg-neutral-950/30 hover:border-neutral-800 transition-colors">
              <span className="text-sm font-bold text-neutral-300">{pipelineJobs.slicing}</span>
              <span className="text-[8px] text-neutral-500 uppercase mt-1">Slicing</span>
            </div>
            
            <div className="flex flex-col items-center p-3 rounded border border-neutral-600 bg-neutral-900 text-white hover:border-neutral-500 transition-colors">
              <span className="text-sm font-bold text-white">{pipelineJobs.transcoding}</span>
              <span className="text-[8px] text-white/80 uppercase mt-1">Transcode</span>
            </div>

            <div className="flex flex-col items-center p-3 rounded border border-neutral-900 bg-neutral-950/30 hover:border-neutral-800 transition-colors">
              <span className="text-sm font-bold text-neutral-300">{pipelineJobs.compiling}</span>
              <span className="text-[8px] text-neutral-500 uppercase mt-1">Compiling</span>
            </div>

            <div className="flex flex-col items-center p-3 rounded border border-neutral-800 bg-neutral-900/40 text-neutral-200 hover:border-neutral-700 transition-colors col-span-2 sm:col-span-1">
              <span className="text-sm font-bold text-neutral-200">{pipelineJobs.completed}</span>
              <span className="text-[8px] text-neutral-400 uppercase mt-1">Finished</span>
            </div>
          </div>

          {/* Compute fleet load heatmap */}
          <div className="border-t border-neutral-900 pt-5">
            <h4 className="text-xs font-mono uppercase tracking-widest text-neutral-450 font-bold mb-3">
              COMPUTE WORKER GPU HEATMAP
            </h4>
            
            <div className="grid grid-cols-5 sm:grid-cols-10 gap-2">
              {workers.length === 0 ? (
                <div className="col-span-10 py-6 text-center text-[10px] font-mono text-neutral-600 uppercase border border-neutral-900 rounded bg-neutral-950">
                  No active workers registered
                </div>
              ) : (
                workers.map(w => {
                  let heatClass = 'bg-neutral-950 border-neutral-900 hover:bg-neutral-900';
                  if (w.gpu > 80) heatClass = 'bg-white border-white text-black hover:bg-neutral-100';
                  else if (w.gpu > 40) heatClass = 'bg-neutral-600 border-neutral-600 text-white hover:bg-neutral-500';
                  else if (w.gpu > 15) heatClass = 'bg-neutral-800 border-neutral-800 text-neutral-300 hover:bg-neutral-750';

                  return (
                    <div 
                      key={w.id} 
                      className={`relative aspect-square rounded border cursor-help group transition-all duration-150 ${heatClass}`}
                    >
                      <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:flex flex-col bg-neutral-950 border border-neutral-800 p-2.5 rounded shadow-2xl text-[9px] font-mono text-neutral-300 w-36 z-30 pointer-events-none">
                        <strong className="text-white border-b border-neutral-800 pb-1 mb-1 block uppercase tracking-wider">{w.id.substring(0, 15)}</strong>
                        <span>GPU LOAD: {w.gpu}%</span>
                        <span>CPU LOAD: {w.cpu}%</span>
                        <span>ACTIVE TASKS: {w.tasks}</span>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
            
            <div className="flex gap-4 text-[9px] font-mono text-neutral-500 mt-4 justify-end">
              <span className="flex items-center gap-1">
                <span className="w-2.5 h-2.5 rounded border border-neutral-900 bg-neutral-950" /> IDLE
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2.5 h-2.5 rounded border border-neutral-800 bg-neutral-800" /> ACTIVE
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2.5 h-2.5 rounded border border-neutral-600 bg-neutral-600" /> BUSY
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2.5 h-2.5 rounded border border-white bg-white" /> MAX LOAD
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
