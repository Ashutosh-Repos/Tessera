# 🎛️ Tessera Admin Console

The **Tessera Admin Console** is a mission-control dashboard for managing global transcoding clusters, monitoring multi-region worker fleets, inspecting coordinator partition distributions, and tracking real-time job lifecycles.

---

## 🌟 Key Features

1. **Global Cluster Topology**:
   - Visualizes coordinator hash ring allocations across 1024 partitions.
   - Monitors node join/leave events and partition rebalances via etcd consensus.
2. **Worker Fleet Health**:
   - Real-time CPU, RAM, and hardware accelerator (NVENC / VAAPI / VideoToolbox) utilization.
   - Circuit breaker status (Closed, Open, Half-Open) and Dead Letter Queue (DLQ) depths.
3. **Active Job Monitor**:
   - Live inspection of active transcoding jobs across Slicing, Transcoding, Compiling, and Completed phases.
   - Real-time bit-level segment completion tracking.
4. **Cluster Telemetry**:
   - Direct integration with Prometheus metrics and Grafana alerting webhooks.

---

## 🚀 Quickstart

### Prerequisites
- Node.js >= 18.0.0
- npm or pnpm

### Installation
```bash
cd admin-console
npm install
```

### Local Development
```bash
npm run dev
```
The console will start at `http://localhost:5173`.

### Production Build
```bash
npm run build
npm run preview
```

---

## ⚙️ Environment Configuration

Create a `.env` file in the `admin-console` root:

```ini
# Gateway & Admin API Endpoint
VITE_API_BASE_URL=http://localhost:8080

# Admin API Key for authenticated cluster operations
VITE_ADMIN_API_KEY=admin-secret-key-change-me

# WebSocket Telemetry Stream
VITE_WS_TELEMETRY_URL=ws://localhost:8080/telemetry
```

---

## 📦 Project Structure

```
admin-console/
├── src/
│   ├── components/       # Cluster graphs, Worker fleet cards, Job tables
│   ├── hooks/            # WebSocket telemetry & REST polling hooks
│   ├── services/         # Gateway Admin API client
│   ├── types/            # TypeScript data contracts & models
│   ├── App.tsx           # Dashboard layout & routing
│   └── main.tsx          # Application entrypoint
├── public/               # Static assets & brand icons
├── index.html            # Vite HTML shell
└── vite.config.ts        # Vite build configuration
```
