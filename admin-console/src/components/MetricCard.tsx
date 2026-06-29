import { useEffect, useRef } from 'react';

interface MetricCardProps {
  title: string;
  value: string | number;
  unit?: string;
  trend?: 'up' | 'down' | 'neutral';
  trendValue?: string;
  sparklineData?: number[];
  color?: string; // fallback or custom border accent
  icon?: React.ReactNode;
}

export const MetricCard: React.FC<MetricCardProps> = ({
  title, value, unit, trend, trendValue, sparklineData, icon
}) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    if (!sparklineData || sparklineData.length < 2 || !canvasRef.current) return;
    const canvas = canvasRef.current;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    ctx.scale(dpr, dpr);

    const max = Math.max(...sparklineData, 1);
    const min = Math.min(...sparklineData, 0);
    const range = max - min || 1;
    const step = w / (sparklineData.length - 1);

    // B&W Gradient fill
    const gradient = ctx.createLinearGradient(0, 0, 0, h);
    gradient.addColorStop(0, 'rgba(255, 255, 255, 0.08)');
    gradient.addColorStop(1, 'transparent');

    ctx.beginPath();
    ctx.moveTo(0, h);
    sparklineData.forEach((v, i) => {
      const x = i * step;
      const y = h - ((v - min) / range) * h * 0.75; // leave some top padding
      ctx.lineTo(x, y);
    });
    ctx.lineTo(w, h);
    ctx.closePath();
    ctx.fillStyle = gradient;
    ctx.fill();

    // Pure White Line for sparkline
    ctx.beginPath();
    sparklineData.forEach((v, i) => {
      const x = i * step;
      const y = h - ((v - min) / range) * h * 0.75;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = '#ffffff';
    ctx.lineWidth = 1.25;
    ctx.lineJoin = 'round';
    ctx.stroke();
  }, [sparklineData]);

  return (
    <div className="relative overflow-hidden rounded border border-neutral-900 bg-neutral-950 p-6 shadow-sm flex flex-col justify-between min-h-[140px] hover:border-neutral-800 transition-colors duration-200">
      <div>
        <div className="flex items-center justify-between mb-3">
          <span className="text-[10px] font-mono uppercase tracking-widest text-neutral-400 font-bold">{title}</span>
          {icon && <div className="text-neutral-400">{icon}</div>}
        </div>
        <div className="text-2xl font-bold font-mono text-white flex items-baseline gap-0.5">
          <span>{value}</span>
          {unit && <span className="text-xs text-neutral-500 font-normal ml-1 font-mono uppercase">{unit}</span>}
        </div>
      </div>
      {trend && (
        <div className="text-[9px] font-mono px-2 py-0.5 rounded border border-neutral-850 w-max z-10 text-neutral-300 bg-neutral-900/60 uppercase tracking-widest">
          {trend === 'up' ? '↑' : trend === 'down' ? '↓' : '→'} {trendValue}
        </div>
      )}
      {sparklineData && sparklineData.length > 1 && (
        <canvas ref={canvasRef} className="absolute bottom-0 inset-x-0 h-10 w-full opacity-40 pointer-events-none" style={{ height: '40px' }} />
      )}
    </div>
  );
};
