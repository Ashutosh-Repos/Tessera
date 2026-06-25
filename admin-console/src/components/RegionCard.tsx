import React from 'react';

interface RegionCardProps {
  name: string;
  code: string;
  gateways: number;
  coordinators: number;
  workers: number;
  healthy: boolean;
  services: { redis: boolean; nats: boolean; s3: boolean; etcd: boolean };
  uploadsPerMin?: number;
  jobsActive?: number;
}

export const RegionCard: React.FC<RegionCardProps> = ({
  name, code, gateways, coordinators, workers, healthy, services, uploadsPerMin = 0, jobsActive = 0
}) => {
  return (
    <div className={`rounded-xl bg-zinc-950/40 p-5 shadow-lg backdrop-blur-sm border transition-all duration-200 hover:border-zinc-800 ${
      healthy ? 'border-zinc-900' : 'border-rose-900 bg-rose-950/5'
    }`}>
      {/* Header */}
      <div className="flex items-center justify-between pb-3 border-b border-zinc-900/50 mb-3.5">
        <div className="flex items-center gap-2">
          <div className={`h-2.5 w-2.5 rounded-full ${
            healthy ? 'bg-emerald-500 shadow-md shadow-emerald-500/30' : 'bg-rose-500 shadow-md shadow-rose-500/30'
          }`} />
          <h3 className="text-sm font-bold font-mono text-zinc-200">{name}</h3>
        </div>
        <span className="text-[10px] font-mono text-zinc-500 bg-zinc-900 px-2.5 py-0.5 rounded border border-zinc-800/80">
          {code}
        </span>
      </div>

      {/* Nodes Count */}
      <div className="grid grid-cols-3 gap-2 text-center text-xs py-2 bg-zinc-900/20 rounded-lg border border-zinc-900/40 mb-3.5">
        <div className="flex flex-col items-center">
          <span className="text-[9px] font-mono uppercase tracking-wider text-zinc-500">Gateway</span>
          <span className="text-xs font-bold text-zinc-300 font-mono mt-0.5">×{gateways}</span>
        </div>
        <div className="flex flex-col items-center border-x border-zinc-900/50">
          <span className="text-[9px] font-mono uppercase tracking-wider text-zinc-500">Coord</span>
          <span className="text-xs font-bold text-zinc-300 font-mono mt-0.5">×{coordinators}</span>
        </div>
        <div className="flex flex-col items-center">
          <span className="text-[9px] font-mono uppercase tracking-wider text-zinc-500">Worker</span>
          <span className="text-xs font-bold text-zinc-300 font-mono mt-0.5">×{workers}</span>
        </div>
      </div>

      {/* Services Health */}
      <div className="flex flex-wrap gap-2 mb-3.5">
        {Object.entries(services).map(([svc, ok]) => (
          <div 
            key={svc} 
            className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full border text-[9px] font-mono font-bold transition-colors ${
              ok 
                ? 'bg-emerald-500/5 text-emerald-400 border-emerald-950/20' 
                : 'bg-rose-500/5 text-rose-400 border-rose-950/20'
            }`}
          >
            <div className={`h-1.5 w-1.5 rounded-full ${ok ? 'bg-emerald-500' : 'bg-rose-500'}`} />
            <span>{svc.toUpperCase()}</span>
          </div>
        ))}
      </div>

      {/* Live Metrics */}
      <div className="grid grid-cols-2 gap-4 pt-3 border-t border-zinc-900/50 text-center">
        <div className="flex flex-col">
          <span className="text-base font-bold font-mono text-zinc-200">{uploadsPerMin}</span>
          <span className="text-[10px] font-mono text-zinc-500">uploads/min</span>
        </div>
        <div className="flex flex-col border-l border-zinc-900/50">
          <span className="text-base font-bold font-mono text-zinc-200">{jobsActive}</span>
          <span className="text-[10px] font-mono text-zinc-500">active jobs</span>
        </div>
      </div>
    </div>
  );
};
