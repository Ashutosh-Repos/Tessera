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
    <div className="flex min-h-screen bg-zinc-950 text-zinc-100 font-sans">
      {/* Sidebar Navigation */}
      <aside className="w-64 border-r border-zinc-900 bg-zinc-950 flex flex-col justify-between select-none shrink-0">
        <div>
          {/* Brand header */}
          <div className="h-16 flex items-center gap-3 px-6 border-b border-zinc-900">
            <div className="rounded-lg bg-purple-500/10 p-1.5 text-purple-500 border border-purple-900/30">
              <HardDrive className="h-4.5 w-4.5" />
            </div>
            <span className="font-bold text-sm tracking-tight font-mono text-zinc-100 uppercase">Transcoder SRE</span>
          </div>

          {/* Navigation link list */}
          <nav className="py-6 px-4 space-y-1">
            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded-md text-xs font-mono transition-all ${
                activeTab === 'dashboard'
                  ? 'text-zinc-100 bg-zinc-900 border border-zinc-800'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('dashboard')}
            >
              <BarChart3 className="h-4 w-4" />
              <span>Fleet Dashboard</span>
            </button>

            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded-md text-xs font-mono transition-all ${
                activeTab === 'jobs'
                  ? 'text-zinc-100 bg-zinc-900 border border-zinc-800'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('jobs')}
            >
              <ListOrdered className="h-4 w-4" />
              <span>Active Pipelines</span>
            </button>

            <button
              className={`w-full flex items-center gap-3 px-3 py-2 rounded-md text-xs font-mono transition-all ${
                activeTab === 'topology'
                  ? 'text-zinc-100 bg-zinc-900 border border-zinc-800'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/30 border border-transparent'
              }`}
              onClick={() => setActiveTab('topology')}
            >
              <Network className="h-4 w-4" />
              <span>Ring Topology</span>
            </button>
          </nav>
        </div>

        {/* Sidebar Footer */}
        <div className="p-4 border-t border-zinc-900 flex flex-col gap-1.5">
          <div className="flex items-center gap-2 text-[10px] font-mono text-zinc-400">
            <span className="h-2 w-2 rounded-full bg-emerald-500 shadow shadow-emerald-500/30" />
            <span>All Systems Nominal</span>
          </div>
          <span className="text-[9px] font-mono text-zinc-600">v1.2.0-sre-tailwind</span>
        </div>
      </aside>

      {/* Main Content Area */}
      <main className="flex-1 flex flex-col min-w-0">
        {/* Header bar */}
        <header className="h-16 flex items-center justify-between px-8 border-b border-zinc-900 bg-zinc-950">
          <h1 className="text-xs font-bold tracking-wider text-zinc-400 uppercase font-mono">
            {activeTab === 'dashboard' && 'Fleet Telemetry'}
            {activeTab === 'jobs' && 'Active Pipelines'}
            {activeTab === 'topology' && 'Ring Topology'}
          </h1>
          <div className="flex items-center gap-3 text-[10px] font-mono text-zinc-500">
            <span>Region Scope: <strong className="text-zinc-400">GLOBAL</strong></span>
            <span className="text-zinc-800">|</span>
            <span>Role: <strong className="text-zinc-400">SRE Admin</strong></span>
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
