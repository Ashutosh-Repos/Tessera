import { useEffect, useRef } from 'react';

interface MetricCardProps {
  title: string;
  value: string | number;
  unit?: string;
  trend?: 'up' | 'down' | 'neutral';
  trendValue?: string;
  sparklineData?: number[];
  color?: string;
  icon?: React.ReactNode;
}

export const MetricCard: React.FC<MetricCardProps> = ({
  title, value, unit, trend, trendValue, sparklineData, color = '#818CF8', icon
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

    // Gradient fill
    const gradient = ctx.createLinearGradient(0, 0, 0, h);
    gradient.addColorStop(0, color + '20');
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

    // Line
    ctx.beginPath();
    sparklineData.forEach((v, i) => {
      const x = i * step;
      const y = h - ((v - min) / range) * h * 0.75;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.5;
    ctx.lineJoin = 'round';
    ctx.stroke();
  }, [sparklineData, color]);

  const getTrendClass = () => {
    if (trend === 'up') return 'text-emerald-400 bg-emerald-500/10 border-emerald-950/30';
    if (trend === 'down') return 'text-rose-400 bg-rose-500/10 border-rose-950/30';
    return 'text-zinc-400 bg-zinc-800/40 border-zinc-800/30';
  };

  return (
    <div className="relative overflow-hidden rounded-xl border border-zinc-900 bg-zinc-950/40 p-6 backdrop-blur-sm shadow-xl flex flex-col justify-between min-h-[140px] hover:border-zinc-800 transition-colors duration-200">
      <div>
        <div className="flex items-center gap-2 mb-2">
          {icon && <div className="p-1 rounded bg-zinc-900/60" style={{ color }}>{icon}</div>}
          <span className="text-[10px] font-mono uppercase tracking-wider text-zinc-400 font-semibold">{title}</span>
        </div>
        <div className="text-2xl font-bold font-mono text-zinc-100 flex items-baseline gap-0.5">
          <span>{value}</span>
          {unit && <span className="text-xs text-zinc-500 font-normal">{unit}</span>}
        </div>
      </div>
      {trend && (
        <div className={`text-[10px] font-mono px-2 py-0.5 rounded border w-max z-10 ${getTrendClass()}`}>
          {trend === 'up' ? '↑' : trend === 'down' ? '↓' : '→'} {trendValue}
        </div>
      )}
      {sparklineData && sparklineData.length > 1 && (
        <canvas ref={canvasRef} className="absolute bottom-0 inset-x-0 h-10 w-full opacity-60 pointer-events-none" style={{ height: '40px' }} />
      )}
    </div>
  );
};
