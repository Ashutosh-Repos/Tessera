# 📚 Tessera Developer Portal

The **Tessera Developer Portal** is an interactive documentation platform, API playground, and developer resource center built with Next.js and Tailwind CSS.

---

## 🌟 Highlights

1. **Interactive API Reference**:
   - Comprehensive documentation for `/api/jobs/upload-session`, `/api/jobs/{uuid}/urls`, `/api/jobs/{uuid}/complete`, and `/api/jobs/{uuid}/status`.
   - Live API request builder with multi-language code snippets (cURL, TypeScript, Python, Go).
2. **Architecture & Integration Guides**:
   - In-depth tutorials explaining zero-bandwidth multipart S3 ingestion, HLS/DASH streaming, and SSE telemetry.
3. **Webhooks & Telemetry**:
   - Documentation for configuring completion webhooks, Dead Letter Queue (DLQ) alerts, and Redis streams.
4. **SDK Downloads**:
   - Integration instructions for `@distributed-transcoder/ui-sdk` and backend client libraries.

---

## 🚀 Getting Started

### Installation
```bash
cd developer-portal
npm install
```

### Local Development Server
```bash
npm run dev
```
Open [http://localhost:3000](http://localhost:3000) with your browser.

### Production Build
```bash
npm run build
npm start
```

---

## ⚙️ Environment Configuration

Create a `.env.local` file in `developer-portal/`:

```ini
# Gateway API URL for interactive sandbox playground
NEXT_PUBLIC_GATEWAY_URL=http://localhost:8080

# Documentation site metadata
NEXT_PUBLIC_SITE_URL=https://docs.tessera.io
```
