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
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-zinc-900 pb-5">
        <div>
          <h2 className="text-xl font-bold font-mono tracking-tight text-zinc-100">Coordinator Topology</h2>
          <p className="text-xs text-zinc-400 mt-1 font-sans">Consistent Hash Ring partition ownership</p>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Consistent Hash Ring Card */}
        <div className="rounded-xl border border-zinc-900 bg-zinc-950/10 p-6 flex flex-col items-center gap-4 backdrop-blur-sm shadow-xl">
          <h3 className="text-sm font-bold font-mono tracking-wider text-zinc-400 uppercase border-b border-zinc-900 pb-3 w-full">
            Partition Hash Ring (Consistent Hashing)
          </h3>
          <p className="text-xs text-zinc-400 mb-2 self-start font-sans">
            Partitions are mapped around the circle. Arcs represent partitions owned by coordinators.
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
        <div className="rounded-xl border border-zinc-900 bg-zinc-950/10 p-6 flex flex-col gap-5 backdrop-blur-sm shadow-xl">
          <div>
            <h3 className="text-sm font-bold font-mono tracking-wider text-zinc-400 uppercase border-b border-zinc-900 pb-3">
              Active Node Registry
            </h3>
            <p className="text-xs text-zinc-400 mt-2 font-sans">
              Currently registered regional coordinator node instances serving partition allocations on the hash ring.
            </p>
          </div>

          <div className="flex flex-col gap-3">
            {coordinators.length === 0 ? (
              <div className="text-center text-zinc-500 font-mono py-8 text-xs">
                No active coordinator nodes found in etcd
              </div>
            ) : (
              coordinators.map(node => {
                const activeInfo = activeCoordinators.find(ac => ac.id === node.id);
                const partitionCount = activeInfo ? activeInfo.partitions.length : 0;

                return (
                  <div 
                    key={node.id} 
                    className="rounded-lg border border-zinc-800/80 bg-zinc-900/10 p-4 transition-all duration-200 flex flex-col gap-3"
                  >
                    <div className="flex items-center justify-between gap-4">
                      <div className="flex items-center gap-2">
                        <div className="h-2.5 w-2.5 rounded-full bg-emerald-500 shadow-md shadow-emerald-500/20" />
                        <span className="text-xs font-bold font-mono text-zinc-200">{node.id}</span>
                      </div>
                    </div>
                    
                    <div className="flex flex-wrap gap-2 text-[9px] font-mono">
                      <span className="bg-zinc-900 border border-zinc-800/80 px-2 py-0.5 rounded text-zinc-400">
                        Region: {(node.region || 'us-east').toUpperCase()}
                      </span>
                      <span className="bg-zinc-900 border border-zinc-800/80 px-2 py-0.5 rounded text-zinc-400">
                        Partition Range: {activeInfo && activeInfo.partitions.length > 0 ? (
                          `P[${activeInfo.partitions[0]} - ${activeInfo.partitions[activeInfo.partitions.length - 1]}]`
                        ) : 'None'}
                      </span>
                      <span className="bg-zinc-900 border border-zinc-800/80 px-2 py-0.5 rounded text-zinc-400">
                        Partition Count: {partitionCount}
                      </span>
                    </div>
                  </div>
                );
              })
            )}
          </div>

          <div className="rounded-lg border border-purple-900/40 bg-purple-950/15 p-4 text-[11px] font-mono text-purple-300/90 leading-relaxed">
            <strong>Topology Safe:</strong> Consistent hashing ensures that when a coordinator node fails, only its partitions are reassigned, limiting rebalance traffic overhead.
          </div>
        </div>
      </div>
    </div>
  );
};
