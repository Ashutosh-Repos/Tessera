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
    <div className={`rounded border bg-neutral-950 p-5 shadow-sm transition-all duration-200 ${
      healthy ? 'border-neutral-900 hover:border-neutral-750' : 'border-neutral-800 bg-neutral-950/70'
    }`}>
      {/* Header */}
      <div className="flex items-center justify-between pb-3 border-b border-neutral-900 mb-4">
        <div className="flex items-center gap-2">
          <div className={`h-2 w-2 rounded-full ${
            healthy ? 'bg-white' : 'bg-neutral-600 animate-pulse'
          }`} />
          <h3 className="text-xs font-bold font-mono text-neutral-200 uppercase tracking-widest">{name}</h3>
        </div>
        <span className="text-[9px] font-mono text-neutral-400 bg-neutral-900 px-2 py-0.5 rounded border border-neutral-850">
          {code}
        </span>
      </div>

      {/* Nodes Count */}
      <div className="grid grid-cols-3 gap-2 text-center text-[10px] py-2 bg-neutral-900/30 rounded border border-neutral-900 mb-4 font-mono">
        <div className="flex flex-col items-center">
          <span className="text-[8px] uppercase tracking-wider text-neutral-500">Gateway</span>
          <span className="font-bold text-neutral-200 mt-0.5">×{gateways}</span>
        </div>
        <div className="flex flex-col items-center border-x border-neutral-900">
          <span className="text-[8px] uppercase tracking-wider text-neutral-500">Coord</span>
          <span className="font-bold text-neutral-200 mt-0.5">×{coordinators}</span>
        </div>
        <div className="flex flex-col items-center">
          <span className="text-[8px] uppercase tracking-wider text-neutral-500">Worker</span>
          <span className="font-bold text-neutral-200 mt-0.5">×{workers}</span>
        </div>
      </div>

      {/* Services Health */}
      <div className="flex flex-wrap gap-1.5 mb-4">
        {Object.entries(services).map(([svc, ok]) => (
          <div 
            key={svc} 
            className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded border text-[9px] font-mono font-semibold transition-colors ${
              ok 
                ? 'bg-neutral-900 text-white border-neutral-800' 
                : 'bg-neutral-950 text-neutral-600 border-neutral-900 border-dashed'
            }`}
          >
            <div className={`h-1 w-1 rounded-full ${ok ? 'bg-white' : 'bg-neutral-700'}`} />
            <span>{svc.toUpperCase()}</span>
          </div>
        ))}
      </div>

      {/* Live Metrics */}
      <div className="grid grid-cols-2 gap-4 pt-3 border-t border-neutral-900 text-center font-mono">
        <div className="flex flex-col">
          <span className="text-sm font-bold text-neutral-200">{uploadsPerMin}</span>
          <span className="text-[9px] uppercase tracking-widest text-neutral-500">uploads/m</span>
        </div>
        <div className="flex flex-col border-l border-neutral-900">
          <span className="text-sm font-bold text-neutral-200">{jobsActive}</span>
          <span className="text-[9px] uppercase tracking-widest text-neutral-500">active j</span>
        </div>
      </div>
    </div>
  );
};
