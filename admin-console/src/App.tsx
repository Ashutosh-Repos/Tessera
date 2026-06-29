import { useState } from 'react';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Topology } from './pages/Topology';
import { BarChart3, ListOrdered, Network, HardDrive } from 'lucide-react';

type Tab = 'dashboard' | 'jobs' | 'topology';

function App() {
  const [activeTab, setActiveTab] = useState<Tab>('dashboard');

  const renderContent = () => {
    switch (activeTab) {
      case 'dashboard':
        return <Dashboard />;
      case 'jobs':
        return <Jobs />;
      case 'topology':
        return <Topology />;
      default:
        return <Dashboard />;
    }
  };

  return (
    <div className="flex min-h-screen bg-black text-neutral-100 font-sans selection:bg-neutral-800 selection:text-white">
      {/* Sidebar Navigation */}
      <aside className="w-64 border-r border-neutral-900 bg-black flex flex-col justify-between select-none shrink-0">
        <div>
          {/* Brand header */}
          <div className="h-16 flex items-center gap-3 px-6 border-b border-neutral-900">
            <div className="rounded bg-neutral-900 p-1.5 text-white border border-neutral-850">
              <HardDrive className="h-4 w-4" />
            </div>
            <span className="font-bold text-sm tracking-widest font-mono text-white uppercase">TRANSCODER_SRE</span>
          </div>

          {/* Navigation link list */}
          <nav className="py-6 px-4 space-y-1">
            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded text-xs font-mono transition-all ${
                activeTab === 'dashboard'
                  ? 'text-white bg-neutral-900 border border-neutral-800'
                  : 'text-neutral-450 hover:text-white hover:bg-neutral-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('dashboard')}
            >
              <BarChart3 className="h-4 w-4" />
              <span>FLEET_DASHBOARD</span>
            </button>

            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded text-xs font-mono transition-all ${
                activeTab === 'jobs'
                  ? 'text-white bg-neutral-900 border border-neutral-800'
                  : 'text-neutral-450 hover:text-white hover:bg-neutral-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('jobs')}
            >
              <ListOrdered className="h-4 w-4" />
              <span>ACTIVE_PIPELINES</span>
            </button>

            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded text-xs font-mono transition-all ${
                activeTab === 'topology'
                  ? 'text-white bg-neutral-900 border border-neutral-800'
                  : 'text-neutral-450 hover:text-white hover:bg-neutral-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('topology')}
            >
              <Network className="h-4 w-4" />
              <span>RING_TOPOLOGY</span>
            </button>
          </nav>
        </div>

        {/* Sidebar Footer */}
        <div className="p-4 border-t border-neutral-900 flex flex-col gap-1.5">
          <div className="flex items-center gap-2 text-[10px] font-mono text-neutral-400">
            <span className="h-1.5 w-1.5 rounded-full bg-white" />
            <span>ALL SYSTEMS NOMINAL</span>
          </div>
          <span className="text-[8px] font-mono text-neutral-600 uppercase tracking-wider">v1.2.0-SRE-MINIMAL</span>
        </div>
      </aside>

      {/* Main Content Area */}
      <main className="flex-1 flex flex-col min-w-0 bg-black">
        {/* Header bar */}
        <header className="h-16 flex items-center justify-between px-8 border-b border-neutral-900 bg-black">
          <h1 className="text-xs font-bold tracking-widest text-neutral-450 uppercase font-mono">
            {activeTab === 'dashboard' && 'FLEET TELEMETRY'}
            {activeTab === 'jobs' && 'ACTIVE PIPELINES'}
            {activeTab === 'topology' && 'RING TOPOLOGY'}
          </h1>
          <div className="flex items-center gap-3 text-[9px] font-mono text-neutral-500 uppercase tracking-widest">
            <span>SCOPE: <strong className="text-neutral-400">GLOBAL</strong></span>
            <span className="text-neutral-800">|</span>
            <span>ROLE: <strong className="text-neutral-400">SRE_ADMIN</strong></span>
          </div>
        </header>

        {/* Page Content */}
        <div className="flex-1 p-8 overflow-y-auto">
          {renderContent()}
        </div>
      </main>
    </div>
  );
}

export default App;
