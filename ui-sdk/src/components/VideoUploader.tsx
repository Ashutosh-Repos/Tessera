import { useState, useRef } from 'react';
import type { DragEvent, ChangeEvent } from 'react';
import { UploadCloud, CheckCircle2, AlertCircle, RefreshCw } from 'lucide-react';

interface VideoUploaderProps {
  gatewayUrl: string; // e.g. http://localhost:8080
  onUploadSuccess?: (hlsUrl: string) => void;
}

export const VideoUploader: React.FC<VideoUploaderProps> = ({ gatewayUrl, onUploadSuccess }) => {
  const [isDragging, setIsDragging] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState<number>(0);
  const [status, setStatus] = useState<'idle' | 'uploading' | 'processing' | 'completed' | 'error'>('idle');
  const [jobId, setJobId] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<string>('');
  const [completedHlsUrl, setCompletedHlsUrl] = useState<string | null>(null);
  
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(true);
  };

  const handleDragLeave = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(false);
  };

  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(false);
    
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      setFile(e.dataTransfer.files[0]);
    }
  };

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      setFile(e.target.files[0]);
    }
  };

  const startUpload = async () => {
    if (!file) return;
    setStatus('uploading');
    setProgress(0);
    setStatusMessage('Initializing Upload...');

    try {
      // Step 1: Create Upload Session
      const sessionRes = await fetch(`${gatewayUrl}/api/jobs/upload-session`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ file_size_bytes: file.size }),
      });
      
      if (!sessionRes.ok) throw new Error('Failed to create upload session');
      const sessionData = await sessionRes.json();
      const token = sessionData.session_token;
      const jId = sessionData.job_id;
      const partSize = sessionData.part_size || (50 * 1024 * 1024);
      const totalParts = sessionData.total_parts;
      
      setJobId(jId);

      // Step 2: Get Presigned URLs
      setStatusMessage('Fetching Upload URLs...');
      const urlsRes = await fetch(`${gatewayUrl}/api/jobs/${jId}/urls?start=1&count=${totalParts}`, {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      
      if (!urlsRes.ok) throw new Error('Failed to get presigned URLs');
      const urlsData = await urlsRes.json();

      // Step 3: Chunk and Upload
      setStatusMessage('Uploading to Edge...');
      const uploadedParts = [];
      let uploadedBytes = 0;

      for (let i = 0; i < totalParts; i++) {
        const start = i * partSize;
        const end = Math.min(start + partSize, file.size);
        const chunk = file.slice(start, end);
        const url = urlsData.urls[i];
        const partNumber = urlsData.part_numbers[i];

        const uploadRes = await fetch(url, {
          method: 'PUT',
          body: chunk,
        });

        if (!uploadRes.ok) throw new Error(`Failed to upload part ${partNumber}`);
        
        let etag = uploadRes.headers.get('ETag');
        if (!etag) {
          etag = `dummy-etag-${partNumber}`;
          console.warn("ETag missing from response headers, using fallback");
        }

        uploadedParts.push({ part_number: partNumber, etag: etag });
        uploadedBytes += chunk.size;
        
        // Update progress just for upload phase (0 to 50%)
        const uploadProgress = Math.floor((uploadedBytes / file.size) * 50);
        setProgress(uploadProgress);
      }

      // Step 4: Complete Upload
      setStatusMessage('Finalizing Upload...');
      const completeRes = await fetch(`${gatewayUrl}/api/jobs/${jId}/complete`, {
        method: 'POST',
        headers: { 
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ parts: uploadedParts }),
      });

      if (!completeRes.ok) throw new Error('Failed to complete upload');

      setStatus('processing');
      setStatusMessage('Distributed Transcoding...');
      
      // Step 5: Listen to SSE for Transcode Progress
      connectToSSE(jId);
      
    } catch (err) {
      console.error(err);
      setStatus('error');
    }
  };

  const connectToSSE = (id: string) => {
    // SSE Progress is mapped from 50% to 100% since upload took 0-50%
    const eventSource = new EventSource(`${gatewayUrl}/progress/${id}`);
    
    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        const percent = data.pct || 0;
        const phase = data.phase || '';

        setProgress(50 + Math.floor(percent / 2));
        
        if (phase === 'COMPLETED') {
          setStatus('completed');
          setProgress(100);
          const finalUrl = data.hls_url;
          setCompletedHlsUrl(finalUrl);
          eventSource.close();
          if (onUploadSuccess && finalUrl) {
            onUploadSuccess(finalUrl);
          }
        }
        if (phase === 'FAILED') {
          setStatus('error');
          setStatusMessage(data.error || 'Transcoding failed');
          eventSource.close();
        }
      } catch (err) {
        console.error("SSE Parse Error", err);
      }
    };

    eventSource.onerror = (err) => {
      console.error("EventSource failed", err);
    };
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const dm = 2;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
  };

  return (
    <div className="w-full max-w-xl mx-auto">
      <div 
        className={`relative overflow-hidden rounded-xl border border-zinc-800 bg-zinc-950/40 p-8 text-center transition-all duration-300 backdrop-blur-sm ${
          isDragging ? 'border-purple-500 bg-purple-500/5' : 'hover:border-zinc-700'
        } ${status !== 'idle' ? 'cursor-default' : 'cursor-pointer'}`}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        onClick={() => status === 'idle' && fileInputRef.current?.click()}
      >
        <input 
          type="file" 
          ref={fileInputRef} 
          className="hidden" 
          accept="video/*" 
          onChange={handleFileChange}
        />
        
        {status === 'idle' && (
          <div className="flex flex-col items-center py-4">
            <div className="mb-4 rounded-full bg-zinc-900 p-4 text-purple-400 ring-1 ring-zinc-800">
              <UploadCloud className="h-8 w-8 animate-pulse" />
            </div>
            
            <h3 className="mb-1 text-lg font-semibold text-zinc-100 font-mono">
              {file ? file.name : "Drag & Drop Video Here"}
            </h3>
            <p className="text-sm text-zinc-400 mb-6">
              {file ? formatBytes(file.size) : "Supports MP4, MOV, AVI up to 50 GB"}
            </p>
            
            {file ? (
              <button 
                className="w-full max-w-xs rounded-md bg-purple-600 px-4 py-2 text-sm font-medium text-white hover:bg-purple-500 active:bg-purple-700 transition-colors shadow-lg shadow-purple-900/30 font-mono"
                onClick={(e) => { e.stopPropagation(); startUpload(); }}
              >
                Upload & Process File
              </button>
            ) : (
              <span className="text-xs text-zinc-500 font-mono bg-zinc-900 px-3 py-1.5 rounded-full border border-zinc-800">
                Click to browse filesystem
              </span>
            )}
          </div>
        )}

        {(status === 'uploading' || status === 'processing') && (
          <div className="flex flex-col items-center py-6">
            <div className="relative mb-6 flex h-24 w-24 items-center justify-center">
              <span className="text-2xl font-bold text-zinc-100 font-mono">{progress}%</span>
              <svg className="absolute inset-0 h-full w-full -rotate-90">
                <circle 
                  className="text-zinc-900" 
                  strokeWidth="4" 
                  stroke="currentColor" 
                  fill="transparent" 
                  r="42" cx="48" cy="48"
                />
                <circle 
                  className="text-purple-500 transition-all duration-300" 
                  strokeWidth="4" 
                  strokeDasharray="263.89" 
                  strokeDashoffset={263.89 - (263.89 * progress) / 100}
                  strokeLinecap="round"
                  stroke="currentColor" 
                  fill="transparent" 
                  r="42" cx="48" cy="48"
                />
              </svg>
            </div>
            
            <h3 className="mb-2 text-lg font-semibold text-zinc-200 font-mono flex items-center gap-2">
              <RefreshCw className="h-4 w-4 animate-spin text-purple-400" />
              {statusMessage}
            </h3>
            {jobId && (
              <p className="text-xs font-mono text-zinc-500 bg-zinc-900/60 px-3 py-1.5 rounded border border-zinc-900">
                Job ID: {jobId}
              </p>
            )}
          </div>
        )}

        {status === 'completed' && (
          <div className="flex flex-col items-center py-6">
            <div className="mb-4 rounded-full bg-emerald-950/50 p-4 text-emerald-400 ring-1 ring-emerald-800">
              <CheckCircle2 className="h-8 w-8" />
            </div>
            <h3 className="mb-1 text-lg font-semibold text-zinc-100 font-mono">Transcoding Complete</h3>
            <p className="text-sm text-zinc-400 mb-6">Your HLS stream is compiled and ready for playback</p>
            
            <button 
              className="w-full max-w-xs rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-500 active:bg-emerald-700 transition-colors shadow-lg shadow-emerald-900/30 font-mono"
              onClick={(e) => { 
                e.stopPropagation(); 
                if (onUploadSuccess && completedHlsUrl) {
                  onUploadSuccess(completedHlsUrl);
                }
              }}
            >
              Watch Stream
            </button>
          </div>
        )}

        {status === 'error' && (
          <div className="flex flex-col items-center py-6">
            <div className="mb-4 rounded-full bg-rose-950/50 p-4 text-rose-400 ring-1 ring-rose-800">
              <AlertCircle className="h-8 w-8" />
            </div>
            <h3 className="mb-1 text-lg font-semibold text-rose-400 font-mono">Processing Failed</h3>
            <p className="text-sm text-zinc-400 mb-6">{statusMessage || "An error occurred during transcoding"}</p>
            
            <button 
              className="w-full max-w-xs rounded-md bg-zinc-800 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700 active:bg-zinc-900 transition-colors border border-zinc-700 font-mono"
              onClick={(e) => { e.stopPropagation(); setStatus('idle'); setFile(null); }}
            >
              Try Again
            </button>
          </div>
        )}
      </div>
    </div>
  );
};
