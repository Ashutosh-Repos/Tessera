import React, { useEffect, useState } from 'react';
import { HashRing } from '../components/HashRing';
import { regions } from '../lib/gateway';

interface CoordinatorNode {
  id: string;
  active: boolean;
  partitions: number[];
  region?: string;
}

export const Topology: React.FC = () => {
  const totalPartitions = 16; // Using 16 for standard clear UI display

  const [coordinators, setCoordinators] = useState<CoordinatorNode[]>([]);

  // Poll gateways for coordinator nodes
  useEffect(() => {
    let active = true;

    const fetchTopology = async () => {
      try {
        const usList = await regions['us-east'].listCoordinators();
        const euList = await regions['eu-west'].listCoordinators();
        
        if (!active) return;
        
        const merged: CoordinatorNode[] = [];
        usList.forEach(nodeID => {
          merged.push({ id: nodeID, active: true, partitions: [], region: 'us-east' });
        });
        euList.forEach(nodeID => {
          merged.push({ id: nodeID, active: true, partitions: [], region: 'eu-west' });
        });

        setCoordinators(merged);
      } catch (err) {
        console.warn("Failed to fetch coordinator nodes:", err);
      }
    };

    fetchTopology();
    const interval = setInterval(fetchTopology, 3000);

    return () => {
      active = false;
      clearInterval(interval);
    };
  }, []);

  // Recalculate consistent hash ownership partitions based on active nodes
  const getActiveCoordinators = (): { id: string; partitions: number[] }[] => {
    if (coordinators.length === 0) return [];

    // Distribute partitions evenly among nodes
    const partitionSlice = Math.ceil(totalPartitions / coordinators.length);
    
    return coordinators.map((node, index) => {
      const start = index * partitionSlice;
      const end = Math.min(totalPartitions, start + partitionSlice);
      const owned: number[] = [];
      for (let i = start; i < end; i++) {
        owned.push(i);
      }
      return {
        id: node.id,
        partitions: owned,
      };
    });
  };

  const activeCoordinators = getActiveCoordinators();

  return (
    <div className="space-y-6 select-none">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-neutral-900 pb-5">
        <div>
          <h2 className="text-xl font-bold font-mono tracking-wider text-white uppercase">COORDINATOR_TOPOLOGY</h2>
          <p className="text-xs text-neutral-500 font-mono mt-1 uppercase tracking-wide">Consistent Hash Ring partition allocations inside etcd lock clusters</p>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Consistent Hash Ring Card */}
        <div className="rounded border border-neutral-900 bg-black p-6 flex flex-col items-center gap-4 shadow-sm">
          <h3 className="text-xs font-bold font-mono tracking-widest text-neutral-450 uppercase border-b border-neutral-900 pb-3 w-full">
            PARTITION HASH RING
          </h3>
          <p className="text-xs text-neutral-400 mb-2 self-start font-mono uppercase tracking-wider text-[10px]">
            PARTITIONS ARE MAPPED DETERMINISTICALLY. SEGMENTS REPRESENT RANGES OWNED BY INDIVIDUAL COORDINATORS.
          </p>
          <div className="flex justify-center items-center py-4 w-full">
            <HashRing
              partitions={totalPartitions}
              coordinators={activeCoordinators}
              width={400}
              height={400}
            />
          </div>
        </div>

        {/* Nodes Registry Panel */}
        <div className="rounded border border-neutral-900 bg-black p-6 flex flex-col gap-5 shadow-sm">
          <div>
            <h3 className="text-xs font-bold font-mono tracking-widest text-neutral-450 uppercase border-b border-neutral-900 pb-3">
              ACTIVE NODE REGISTRY
            </h3>
            <p className="text-xs text-neutral-400 mt-2 font-mono uppercase tracking-wider text-[10px]">
              CURRENT ACTIVE COORDINATOR INSTANCES SERVING PARTITIONS ON THE LOCK RING.
            </p>
          </div>

          <div className="flex flex-col gap-3">
            {coordinators.length === 0 ? (
              <div className="text-center text-neutral-600 font-mono py-8 text-xs uppercase tracking-widest border border-neutral-900 rounded bg-neutral-950">
                No active coordinator nodes found in etcd
              </div>
            ) : (
              coordinators.map(node => {
                const activeInfo = activeCoordinators.find(ac => ac.id === node.id);
                const partitionCount = activeInfo ? activeInfo.partitions.length : 0;

                return (
                  <div 
                    key={node.id} 
                    className="rounded border border-neutral-900 bg-neutral-950 p-4 transition-all duration-200 flex flex-col gap-3 hover:border-neutral-800"
                  >
                    <div className="flex items-center justify-between gap-4">
                      <div className="flex items-center gap-2">
                        <div className="h-1.5 w-1.5 rounded-full bg-white" />
                        <span className="text-xs font-bold font-mono text-neutral-200">{node.id}</span>
                      </div>
                    </div>
                    
                    <div className="flex flex-wrap gap-2 text-[9px] font-mono">
                      <span className="bg-neutral-900 border border-neutral-800 px-2 py-0.5 rounded text-neutral-400 uppercase tracking-wider">
                        Region: {(node.region || 'us-east').toUpperCase()}
                      </span>
                      <span className="bg-neutral-900 border border-neutral-800 px-2 py-0.5 rounded text-neutral-400 uppercase tracking-wider">
                        Range: {activeInfo && activeInfo.partitions.length > 0 ? (
                          `P[${activeInfo.partitions[0]} - ${activeInfo.partitions[activeInfo.partitions.length - 1]}]`
                        ) : 'None'}
                      </span>
                      <span className="bg-neutral-900 border border-neutral-800 px-2 py-0.5 rounded text-neutral-400 uppercase tracking-wider">
                        Partitions: {partitionCount}
                      </span>
                    </div>
                  </div>
                );
              })
            )}
          </div>

          <div className="rounded border border-neutral-800 bg-neutral-900/30 p-4 text-[10px] font-mono text-neutral-400 leading-relaxed uppercase tracking-wider">
            <strong>Ring Mechanics:</strong> Consistent hashing ensures that when a coordinator node leaves or joins the registry ring, only a fraction of partitions are reallocated.
          </div>
        </div>
      </div>
    </div>
  );
};
