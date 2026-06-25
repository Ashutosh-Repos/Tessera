import { useState } from 'react'
import { VideoUploader } from './components/VideoUploader'
import { VideoPlayer } from './components/VideoPlayer'

function App() {
  const GATEWAY_URL = 'http://localhost:8080'
  const DEMO_HLS_URL = 'http://localhost:9000/transcoder-us-east/jobs/partition_3/job_us-east:768244ad-1129-4f0c-944d-3a5cfdf96cf9/master.m3u8'
  
  const [hlsUrl, setHlsUrl] = useState<string>(DEMO_HLS_URL)

  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 flex flex-col items-center justify-start py-12 px-4 sm:px-6 lg:px-8 font-sans">
      <div className="w-full max-w-4xl flex flex-col gap-10">
        
        {/* Header Section */}
        <header className="flex flex-col gap-2 border-b border-zinc-900 pb-8 text-center sm:text-left">
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight font-mono text-zinc-100 flex items-center justify-center sm:justify-start gap-3">
            Video Engine <span className="rounded bg-purple-600/10 px-2.5 py-1 text-xs font-semibold text-purple-400 border border-purple-900/30">SDK</span>
          </h1>
          <p className="text-sm sm:text-base text-zinc-400">
            Premium, drop-in React components for distributed video upload and adaptive bitrate HLS playback.
          </p>
        </header>
        
        {/* Main Sections */}
        <main className="flex flex-col gap-8">
          
          {/* Uploader Section */}
          <section className="bg-zinc-900/25 border border-zinc-900/80 rounded-2xl p-6 sm:p-8 backdrop-blur-sm shadow-xl shadow-black/10 flex flex-col gap-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-zinc-900">
              <h2 className="text-lg font-bold font-mono text-zinc-200">&lt;VideoUploader /&gt;</h2>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded-full border border-zinc-800 bg-zinc-900/80 px-2.5 py-0.5 text-xs font-mono text-zinc-400">
                  S3 Multipart
                </span>
                <span className="inline-flex items-center rounded-full border border-purple-900/40 bg-purple-950/20 px-2.5 py-0.5 text-xs font-mono text-purple-400">
                  Real-Time SSE
                </span>
              </div>
            </div>
            <VideoUploader gatewayUrl={GATEWAY_URL} onUploadSuccess={(url) => setHlsUrl(url)} />
          </section>

          {/* Player Section */}
          <section className="bg-zinc-900/25 border border-zinc-900/80 rounded-2xl p-6 sm:p-8 backdrop-blur-sm shadow-xl shadow-black/10 flex flex-col gap-6">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-zinc-900">
              <h2 className="text-lg font-bold font-mono text-zinc-200">&lt;VideoPlayer /&gt;</h2>
              <div className="flex flex-wrap gap-2">
                <span className="inline-flex items-center rounded-full border border-zinc-800 bg-zinc-900/80 px-2.5 py-0.5 text-xs font-mono text-zinc-400">
                  HLS Adaptive
                </span>
                <span className="inline-flex items-center rounded-full border border-purple-900/40 bg-purple-950/20 px-2.5 py-0.5 text-xs font-mono text-purple-400">
                  Quality Selector
                </span>
              </div>
            </div>
            
            <VideoPlayer hlsUrl={hlsUrl} />
            
            <p className="text-center text-xs text-zinc-500 font-mono mt-2 bg-zinc-950/50 py-3 rounded-lg border border-zinc-900/60">
              Press <kbd className="mx-1 px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800 text-zinc-400">D</kbd> for diagnostics · <kbd className="mx-1 px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800 text-zinc-400">F</kbd> for fullscreen · <kbd className="mx-1 px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800 text-zinc-400">Space</kbd> to play/pause
            </p>
          </section>
        </main>
      </div>
    </div>
  )
}

export default App
