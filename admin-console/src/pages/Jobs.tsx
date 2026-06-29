import React, { useEffect, useState } from 'react';
import { regions } from '../lib/gateway';

interface Job {
  job_id: string;
  region: string;
  phase: string;
  completed: number;
  total: number;
  owner_epoch: number;
  partition_id: number;
  last_updated: number;
}

export const Jobs: React.FC = () => {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [filterRegion, setFilterRegion] = useState<string>('all');
  const [filterPhase, setFilterPhase] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null);

  // Poll gateways for jobs
  useEffect(() => {
    let active = true;

    const fetchJobs = async () => {
      const results: Job[] = [];
      await Promise.all(
        Object.entries(regions).map(async ([regionName, client]) => {
          try {
            const regionalJobs = await client.listJobs();
            if (regionalJobs) {
              results.push(...regionalJobs.map(j => ({ ...j, region: regionName })));
            }
          } catch (e) {
            console.warn(`Failed to fetch jobs from region ${regionName}:`, e);
          }
        })
      );

      if (active && results.length > 0) {
        setJobs(results);
      }
    };

    fetchJobs();
    const interval = setInterval(fetchJobs, 3000);

    return () => {
      active = false;
      clearInterval(interval);
    };
  }, []);

  const getPercent = (j: Job) => {
    if (j.total === 0) return 0;
    return Math.round((j.completed / j.total) * 100);
  };

  const getPhaseBadgeClass = (phase: string) => {
    switch (phase) {
      case 'COMPLETED': return 'bg-white text-black border-white';
      case 'FAILED': return 'bg-neutral-950 text-neutral-500 border-neutral-900 border-dashed';
      case 'TRANSCODING': return 'bg-neutral-900 text-white border-neutral-800';
      case 'SLICING': return 'bg-neutral-950 text-neutral-300 border-neutral-800';
      case 'COMPILING': return 'bg-neutral-900 text-neutral-300 border-neutral-800';
      default: return 'bg-neutral-950 text-neutral-400 border-neutral-900';
    }
  };

  const filteredJobs = jobs.filter(j => {
    const matchRegion = filterRegion === 'all' || j.region === filterRegion;
    const matchPhase = filterPhase === 'all' || j.phase === filterPhase;
    const matchSearch = j.job_id.toLowerCase().includes(searchQuery.toLowerCase());
    return matchRegion && matchPhase && matchSearch;
  });

  return (
    <div className="space-y-6 select-none">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-neutral-900 pb-5">
        <div>
          <h2 className="text-xl font-bold font-mono tracking-wider text-white uppercase">PIPELINE_JOBS</h2>
          <p className="text-xs text-neutral-500 font-mono mt-1 uppercase tracking-wide">Real-time status indexes of job execution across consistent hashing partitions</p>
        </div>
      </div>

      {/* Filters Bar */}
      <div className="grid gap-4 md:grid-cols-3 bg-neutral-950 border border-neutral-900 rounded p-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-[9px] font-mono text-neutral-500 uppercase tracking-widest font-semibold">REGION</label>
          <select 
            value={filterRegion} 
            onChange={(e) => setFilterRegion(e.target.value)}
            className="w-full rounded border border-neutral-800 bg-black px-3 py-1.5 text-xs font-mono text-neutral-300 focus:outline-none focus:ring-1 focus:ring-white"
          >
            <option value="all">ALL REGIONS</option>
            <option value="us-east">US EAST</option>
            <option value="eu-west">EU WEST</option>
          </select>
        </div>
        
        <div className="flex flex-col gap-1.5">
          <label className="text-[9px] font-mono text-neutral-500 uppercase tracking-widest font-semibold">PHASE</label>
          <select 
            value={filterPhase} 
            onChange={(e) => setFilterPhase(e.target.value)}
            className="w-full rounded border border-neutral-800 bg-black px-3 py-1.5 text-xs font-mono text-neutral-300 focus:outline-none focus:ring-1 focus:ring-white"
          >
            <option value="all">ALL PHASES</option>
            <option value="CREATED">CREATED</option>
            <option value="SLICING">SLICING</option>
            <option value="TRANSCODING">TRANSCODING</option>
            <option value="COMPILING">COMPILING</option>
            <option value="COMPLETED">COMPLETED</option>
            <option value="FAILED">FAILED</option>
          </select>
        </div>
        
        <div className="flex flex-col gap-1.5">
          <label className="text-[9px] font-mono text-neutral-500 uppercase tracking-widest font-semibold">SEARCH JOB ID</label>
          <input
            type="text"
            placeholder="Search uuid..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full rounded border border-neutral-800 bg-black px-3 py-1.5 text-xs font-mono text-neutral-300 focus:outline-none focus:ring-1 focus:ring-white placeholder-neutral-700"
          />
        </div>
      </div>

      {/* Jobs Table */}
      <div className="rounded border border-neutral-900 bg-black overflow-hidden shadow-sm">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-neutral-900 bg-neutral-900/30 text-[9px] font-mono text-neutral-400 uppercase tracking-widest">
              <th className="px-4 py-3">JOB ID</th>
              <th className="px-4 py-3">REGION</th>
              <th className="px-4 py-3">PARTITION</th>
              <th className="px-4 py-3">PHASE</th>
              <th className="px-4 py-3">PROGRESS</th>
              <th className="px-4 py-3">UPDATED</th>
              <th className="px-4 py-3 text-right">ACTION</th>
            </tr>
          </thead>
          <tbody>
            {filteredJobs.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-600 font-mono uppercase tracking-widest">
                  No jobs found matching criteria
                </td>
              </tr>
            ) : (
              filteredJobs.map(job => (
                <React.Fragment key={job.job_id}>
                  <tr className={`border-b border-neutral-900/60 transition-colors hover:bg-neutral-900/20 ${
                    expandedJobId === job.job_id ? 'bg-neutral-900/10' : ''
                  }`}>
                    <td className="px-4 py-3 font-mono text-neutral-300 max-w-[200px] truncate" title={job.job_id}>
                      {job.job_id}
                    </td>
                    <td className="px-4 py-3">
                      <span className="px-2 py-0.5 rounded text-[9px] font-mono font-bold border border-neutral-800 bg-neutral-950 text-neutral-400">
                        {job.region.toUpperCase()}
                      </span>
                    </td>
                    <td className="px-4 py-3 font-mono text-neutral-300">{job.partition_id}</td>
                    <td className="px-4 py-3">
                      <span className={`px-2 py-0.5 rounded text-[9px] font-mono font-bold border inline-block ${getPhaseBadgeClass(job.phase)}`}>
                        {job.phase}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-3 w-48 font-mono">
                        <div className="w-full bg-neutral-900 rounded-full h-1 overflow-hidden border border-neutral-950">
                          <div
                            className="bg-white h-full transition-all duration-300"
                            style={{ width: `${getPercent(job)}%` }}
                          />
                        </div>
                        <span className="text-[9px] text-neutral-500 shrink-0">
                          {job.completed}/{job.total}
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-neutral-400">
                      {new Date(job.last_updated * 1000).toLocaleTimeString()}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        className="text-white hover:underline font-mono text-[9px] font-semibold uppercase tracking-widest"
                        onClick={() => setExpandedJobId(expandedJobId === job.job_id ? null : job.job_id)}
                      >
                        {expandedJobId === job.job_id ? '[ HIDE ]' : '[ VIEW ]'}
                      </button>
                    </td>
                  </tr>
                  {expandedJobId === job.job_id && (
                    <tr className="bg-neutral-950/40">
                      <td colSpan={7} className="px-4 py-4 border-b border-neutral-900">
                        <div className="rounded border border-neutral-900 bg-black p-5 space-y-4">
                          <h4 className="text-[10px] font-mono uppercase tracking-widest text-neutral-450 font-bold border-b border-neutral-900 pb-2 mb-2">
                            SEGMENT BITMAPS & METRICS
                          </h4>
                          
                          <div className="grid gap-4 sm:grid-cols-2 text-[10px] font-mono text-neutral-400">
                            <div className="space-y-1.5">
                              <div>COORDINATOR EPOCH: <span className="text-white font-semibold">{job.owner_epoch}</span></div>
                              <div>TARGET RESOLUTIONS: <span className="text-white font-semibold">1080P, 720P, 480P</span></div>
                              <div>MULTIPART CHUNK LIMIT: <span className="text-white font-semibold">50MB PER PART</span></div>
                            </div>
                            <div className="space-y-2">
                              <div>
                                HLS PLAYLIST ENTRYPOINT:
                                <code className="block mt-1 p-1.5 bg-neutral-950 border border-neutral-900 rounded text-neutral-300 text-[9px] break-all select-all">
                                  /jobs/partition_{job.partition_id}/job_{job.job_id}/hls/master.m3u8
                                </code>
                              </div>
                              <div>
                                DASH MANIFEST ENTRYPOINT:
                                <code className="block mt-1 p-1.5 bg-neutral-950 border border-neutral-900 rounded text-neutral-300 text-[9px] break-all select-all">
                                  /jobs/partition_{job.partition_id}/job_{job.job_id}/dash/manifest.mpd
                                </code>
                              </div>
                            </div>
                          </div>

                          <div className="space-y-2 pt-3 border-t border-neutral-900">
                            <h5 className="text-[9px] font-mono text-neutral-400 uppercase tracking-widest font-semibold">Segment Progress Matrix</h5>
                            <div className="flex flex-wrap gap-1.5 py-1">
                              {Array.from({ length: job.total || 10 }).map((_, i) => {
                                let boxClass = 'bg-neutral-950 border-neutral-900';
                                if (job.phase === 'COMPLETED') boxClass = 'bg-white border-white';
                                else if (job.phase === 'FAILED') boxClass = i < job.completed ? 'bg-neutral-500 border-neutral-500' : 'bg-neutral-950 border-neutral-900 border-dashed';
                                else if (i < job.completed) boxClass = 'bg-neutral-300 border-neutral-300';
                                else if (i === job.completed) boxClass = 'bg-white border-white animate-pulse';

                                return (
                                  <div
                                    key={i}
                                    className={`w-3 h-3 rounded-sm border transition-all duration-150 ${boxClass}`}
                                    title={`Segment ${i + 1}`}
                                  />
                                );
                              })}
                            </div>
                            
                            <div className="flex gap-4 text-[9px] font-mono text-neutral-500 justify-start pt-1">
                              <span className="flex items-center gap-1">
                                <span className="w-2 h-2 rounded-sm border border-neutral-300 bg-neutral-300" /> COMPLETED
                              </span>
                              <span className="flex items-center gap-1">
                                <span className="w-2 h-2 rounded-sm border border-white bg-white animate-pulse" /> PROCESSING
                              </span>
                              <span className="flex items-center gap-1">
                                <span className="w-2 h-2 rounded-sm border border-neutral-900 bg-neutral-950" /> QUEUED
                              </span>
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
