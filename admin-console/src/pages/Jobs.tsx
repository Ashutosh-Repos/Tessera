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
      case 'COMPLETED': return 'bg-emerald-500/5 text-emerald-400 border-emerald-950/20';
      case 'FAILED': return 'bg-rose-500/5 text-rose-400 border-rose-950/20';
      case 'TRANSCODING': return 'bg-purple-500/5 text-purple-400 border-purple-950/20';
      case 'SLICING': return 'bg-amber-500/5 text-amber-400 border-amber-950/20';
      case 'COMPILING': return 'bg-cyan-500/5 text-cyan-400 border-cyan-950/20';
      default: return 'bg-zinc-800/40 text-zinc-400 border-zinc-800/30';
    }
  };

  const filteredJobs = jobs.filter(j => {
    const matchRegion = filterRegion === 'all' || j.region === filterRegion;
    const matchPhase = filterPhase === 'all' || j.phase === filterPhase;
    const matchSearch = j.job_id.toLowerCase().includes(searchQuery.toLowerCase());
    return matchRegion && matchPhase && matchSearch;
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-zinc-900 pb-5">
        <div>
          <h2 className="text-xl font-bold font-mono tracking-tight text-zinc-100">Transcoding Pipeline Jobs</h2>
          <p className="text-xs text-zinc-400 mt-1 font-sans">Real-time job execution state across partitions</p>
        </div>
      </div>

      {/* Filters Bar */}
      <div className="grid gap-4 md:grid-cols-3 bg-zinc-950/20 border border-zinc-900 rounded-xl p-4">
        <div className="flex flex-col gap-1.5">
          <label className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider font-semibold">Region</label>
          <select 
            value={filterRegion} 
            onChange={(e) => setFilterRegion(e.target.value)}
            className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-xs font-mono text-zinc-300 focus:outline-none focus:ring-1 focus:ring-purple-500"
          >
            <option value="all">All Regions</option>
            <option value="us-east">US East</option>
            <option value="eu-west">EU West</option>
          </select>
        </div>
        
        <div className="flex flex-col gap-1.5">
          <label className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider font-semibold">Phase</label>
          <select 
            value={filterPhase} 
            onChange={(e) => setFilterPhase(e.target.value)}
            className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-xs font-mono text-zinc-300 focus:outline-none focus:ring-1 focus:ring-purple-500"
          >
            <option value="all">All Phases</option>
            <option value="CREATED">Created</option>
            <option value="SLICING">Slicing</option>
            <option value="TRANSCODING">Transcoding</option>
            <option value="COMPILING">Compiling</option>
            <option value="COMPLETED">Completed</option>
            <option value="FAILED">Failed</option>
          </select>
        </div>
        
        <div className="flex flex-col gap-1.5">
          <label className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider font-semibold">Search Job ID</label>
          <input
            type="text"
            placeholder="Search uuid..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-xs font-mono text-zinc-300 focus:outline-none focus:ring-1 focus:ring-purple-500 placeholder-zinc-700"
          />
        </div>
      </div>

      {/* Jobs Table */}
      <div className="rounded-xl border border-zinc-900 bg-zinc-950/10 backdrop-blur-sm overflow-hidden shadow-2xl">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-zinc-900 bg-zinc-900/30 text-[10px] font-mono text-zinc-400 uppercase tracking-wider">
              <th className="px-4 py-3">Job ID</th>
              <th className="px-4 py-3">Region</th>
              <th className="px-4 py-3">Partition</th>
              <th className="px-4 py-3">Phase</th>
              <th className="px-4 py-3">Progress</th>
              <th className="px-4 py-3">Last Updated</th>
              <th className="px-4 py-3">Action</th>
            </tr>
          </thead>
          <tbody>
            {filteredJobs.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-zinc-500 font-mono">
                  No jobs found matching criteria
                </td>
              </tr>
            ) : (
              filteredJobs.map(job => (
                <React.Fragment key={job.job_id}>
                  <tr className={`border-b border-zinc-900/40 transition-colors hover:bg-zinc-900/10 ${
                    expandedJobId === job.job_id ? 'bg-zinc-900/10' : ''
                  }`}>
                    <td className="px-4 py-3 font-mono text-zinc-300 max-w-[200px] truncate" title={job.job_id}>
                      {job.job_id}
                    </td>
                    <td className="px-4 py-3">
                      <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold border border-zinc-800 bg-zinc-900 text-zinc-400">
                        {job.region.toUpperCase()}
                      </span>
                    </td>
                    <td className="px-4 py-3 font-mono text-zinc-300">{job.partition_id}</td>
                    <td className="px-4 py-3">
                      <span className={`px-2.5 py-0.5 rounded-full text-[10px] font-mono font-semibold border inline-block ${getPhaseBadgeClass(job.phase)}`}>
                        {job.phase}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-3 w-48">
                        <div className="w-full bg-zinc-900 rounded-full h-1.5 border border-zinc-800/80 overflow-hidden">
                          <div
                            className="bg-purple-500 h-full transition-all duration-300"
                            style={{ width: `${getPercent(job)}%` }}
                          />
                        </div>
                        <span className="text-[9px] font-mono text-zinc-500 shrink-0">
                          {job.completed}/{job.total} ({getPercent(job)}%)
                        </span>
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-zinc-400">
                      {new Date(job.last_updated * 1000).toLocaleTimeString()}
                    </td>
                    <td className="px-4 py-3">
                      <button
                        className="text-purple-400 hover:text-purple-300 font-mono text-[10px] font-medium"
                        onClick={() => setExpandedJobId(expandedJobId === job.job_id ? null : job.job_id)}
                      >
                        {expandedJobId === job.job_id ? 'Hide Details' : 'View Details'}
                      </button>
                    </td>
                  </tr>
                  {expandedJobId === job.job_id && (
                    <tr className="bg-zinc-950/20">
                      <td colSpan={7} className="px-4 py-4 border-b border-zinc-900/60">
                        <div className="rounded-lg border border-zinc-900 bg-zinc-950/40 p-5 space-y-4">
                          <h4 className="text-xs font-mono uppercase tracking-wider text-zinc-400 font-semibold border-b border-zinc-900 pb-2 mb-2">
                            Segment Bitmaps & Diagnostics
                          </h4>
                          
                          <div className="grid gap-4 sm:grid-cols-2 text-[10px] font-mono text-zinc-400">
                            <div className="space-y-1">
                              <div>Coordinator Epoch: <span className="text-zinc-200">{job.owner_epoch}</span></div>
                              <div>Target Resolutions: <span className="text-zinc-200">1080p, 720p, 480p</span></div>
                              <div>Chunk Size: <span className="text-zinc-200">50MB (Multipart Upload S3)</span></div>
                            </div>
                            <div className="space-y-1">
                              <div>
                                HLS Master Playlist:
                                <code className="block mt-1 p-1.5 bg-zinc-950 border border-zinc-900 rounded text-zinc-300 text-[9px] break-all select-all">
                                  /jobs/partition_{job.partition_id}/job_{job.job_id}/hls/master.m3u8
                                </code>
                              </div>
                              <div>
                                DASH Manifest:
                                <code className="block mt-1 p-1.5 bg-zinc-950 border border-zinc-900 rounded text-zinc-300 text-[9px] break-all select-all">
                                  /jobs/partition_{job.partition_id}/job_{job.job_id}/dash/manifest.mpd
                                </code>
                              </div>
                            </div>
                          </div>

                          <div className="space-y-2 pt-3 border-t border-zinc-900/60">
                            <h5 className="text-[10px] font-mono text-zinc-400 uppercase tracking-wider font-semibold">Segment Status Matrix</h5>
                            <div className="flex flex-wrap gap-1.5 py-1">
                              {Array.from({ length: job.total || 10 }).map((_, i) => {
                                let boxClass = 'bg-zinc-900 border-zinc-800';
                                if (job.phase === 'COMPLETED') boxClass = 'bg-emerald-500/20 border-emerald-500/40 text-emerald-400';
                                else if (job.phase === 'FAILED') boxClass = i < job.completed ? 'bg-emerald-500/20 border-emerald-500/40' : 'bg-rose-500/20 border-rose-500/40';
                                else if (i < job.completed) boxClass = 'bg-emerald-500/20 border-emerald-500/40';
                                else if (i === job.completed) boxClass = 'bg-purple-500/20 border-purple-500/40 animate-pulse';

                                return (
                                  <div
                                    key={i}
                                    className={`w-3.5 h-3.5 rounded border transition-all duration-150 ${boxClass}`}
                                    title={`Segment ${i + 1}`}
                                  />
                                );
                              })}
                            </div>
                            
                            <div className="flex gap-4 text-[9px] font-mono text-zinc-500 justify-start pt-1">
                              <span className="flex items-center gap-1">
                                <span className="w-2.5 h-2.5 rounded border border-emerald-500/40 bg-emerald-500/20" /> Completed
                              </span>
                              <span className="flex items-center gap-1">
                                <span className="w-2.5 h-2.5 rounded border border-purple-500/40 bg-purple-500/20" /> Transcoding
                              </span>
                              <span className="flex items-center gap-1">
                                <span className="w-2.5 h-2.5 rounded border border-zinc-800 bg-zinc-900" /> Queued
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
