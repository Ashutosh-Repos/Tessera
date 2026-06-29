import { useEffect, useRef } from 'react';

interface HashRingProps {
  partitions: number;
  coordinators: { id: string; partitions: number[] }[];
  width?: number;
  height?: number;
}

export const HashRing: React.FC<HashRingProps> = ({
  partitions, coordinators, width = 500, height = 500
}) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);

    const cx = width / 2;
    const cy = height / 2;
    const ringRadius = Math.min(width, height) * 0.38;
    const dotRadius = 2.5;

    // Clear
    ctx.clearRect(0, 0, width, height);

    // Monochromatic shades for coordinator segments
    const colors = [
      '#ffffff', // Stark White
      '#d4d4d4', // Light Gray
      '#a3a3a3', // Neutral Gray
      '#737373', // Dim Gray
      '#525252', // Dark Gray
      '#404040'  // Charcoal
    ];

    // Build ownership map: partition -> coordinator index
    const ownerMap = new Map<number, number>();
    coordinators.forEach((coord, idx) => {
      coord.partitions.forEach(p => ownerMap.set(p, idx));
    });

    // Draw ownership arcs
    coordinators.forEach((coord, idx) => {
      const color = colors[idx % colors.length];
      coord.partitions.forEach(p => {
        const angle = (p / partitions) * Math.PI * 2 - Math.PI / 2;
        const nextAngle = ((p + 1) / partitions) * Math.PI * 2 - Math.PI / 2;

        ctx.beginPath();
        ctx.arc(cx, cy, ringRadius - 6, angle, nextAngle);
        ctx.strokeStyle = color + '25'; // subtle arc fill
        ctx.lineWidth = 10;
        ctx.stroke();
      });
    });

    // Draw ring base line
    ctx.beginPath();
    ctx.arc(cx, cy, ringRadius, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.05)';
    ctx.lineWidth = 1;
    ctx.stroke();

    // Draw partition dots
    for (let i = 0; i < partitions; i++) {
      const angle = (i / partitions) * Math.PI * 2 - Math.PI / 2;
      const x = cx + ringRadius * Math.cos(angle);
      const y = cy + ringRadius * Math.sin(angle);
      const ownerIdx = ownerMap.get(i);
      const color = ownerIdx !== undefined ? colors[ownerIdx % colors.length] : 'rgba(255, 255, 255, 0.15)';

      ctx.beginPath();
      ctx.arc(x, y, dotRadius, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
    }

    // Draw coordinator labels
    coordinators.forEach((coord, idx) => {
      const color = colors[idx % colors.length];
      const avgPartition = coord.partitions.length > 0
        ? coord.partitions.reduce((a, b) => a + b, 0) / coord.partitions.length
        : 0;
      const angle = (avgPartition / partitions) * Math.PI * 2 - Math.PI / 2;
      const labelRadius = ringRadius + 28;
      const x = cx + labelRadius * Math.cos(angle);
      const y = cy + labelRadius * Math.sin(angle);

      // Label Dot
      ctx.beginPath();
      ctx.arc(x, y, 4, 0, Math.PI * 2);
      ctx.fillStyle = color;
      ctx.fill();
      ctx.strokeStyle = '#000000';
      ctx.lineWidth = 1.5;
      ctx.stroke();

      // Label Text
      ctx.fillStyle = 'rgba(255, 255, 255, 0.65)';
      ctx.font = '8px ui-monospace, SFMono-Regular, monospace';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      const labelX = cx + (labelRadius + 18) * Math.cos(angle);
      const labelY = cy + (labelRadius + 18) * Math.sin(angle);
      ctx.fillText(coord.id.substring(0, 10).toUpperCase(), labelX, labelY);
    });

    // Center text
    ctx.fillStyle = '#ffffff';
    ctx.font = 'bold 20px ui-monospace, SFMono-Regular, monospace';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(`${partitions}`, cx, cy - 6);
    ctx.fillStyle = 'rgba(255, 255, 255, 0.4)';
    ctx.font = '8px ui-monospace, SFMono-Regular, monospace';
    ctx.fillText('PARTITIONS', cx, cy + 12);

  }, [partitions, coordinators, width, height]);

  return (
    <div className="flex justify-center items-center w-full h-full">
      <canvas ref={canvasRef} className="block max-w-full h-auto rounded bg-transparent" style={{ width, height }} />
    </div>
  );
};
