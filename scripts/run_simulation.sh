#!/usr/bin/env bash

# Exit immediately if any command fails
set -e

# Define paths and directories
WORKSPACE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOGS_DIR="$WORKSPACE_DIR/logs"
SCRATCH_EAST="/tmp/scratch-us-east"
SCRATCH_WEST="/tmp/scratch-eu-west"

echo "======================================================================"
echo " 🌐 DISTRIBUTED TRANSCODER: MULTI-REGION SIMULATION RUNNER"
echo "======================================================================"

# 1. Clean up stale state and processes
cleanup() {
  echo ""
  echo "======================================================================"
  echo " 🛑 TEARDOWN: Cleaning up processes and Docker containers..."
  echo "======================================================================"
  
  # Terminate all background jobs launched by this script group
  jobs -p | xargs kill -9 2>/dev/null || true
  
  # Stop Docker Compose
  docker compose -f "$WORKSPACE_DIR/docker-compose-multiregion.yml" down -v || true
  
  # Clean up scratch folders
  rm -rf "$SCRATCH_EAST" "$SCRATCH_WEST"
  
  echo "✅ Cleanup complete. Exiting."
}

# Register traps to trigger cleanup on exit signals
trap cleanup EXIT SIGINT SIGTERM

# 2. Setup directories
mkdir -p "$LOGS_DIR"
rm -rf "$LOGS_DIR"/*
mkdir -p "$SCRATCH_EAST" "$SCRATCH_WEST"

# 3. Spin up docker-compose infrastructure
echo "🐳 1. Starting Redis, NATS, etcd, and MinIO clusters..."
docker compose -f "$WORKSPACE_DIR/docker-compose-multiregion.yml" up -d

echo "⏳ Waiting 8 seconds for infrastructure services to initialize..."
sleep 8

# 4. Build transcoder application
echo "🔨 2. Compiling transcoder Go binary..."
go build -o "$WORKSPACE_DIR/transcoder-bin" "$WORKSPACE_DIR/cmd/transcoder/main.go"

# 5. Start Regional Daemons
echo "🚀 3. Booting Regional Node fleets in the background..."

# --- Region A: US-East ---
echo "     👉 Booting US-East Nodes..."
"$WORKSPACE_DIR/transcoder-bin" server gateway --config "$WORKSPACE_DIR/configs/us-east.yaml" --region us-east > "$LOGS_DIR/us-east-gateway.log" 2>&1 &
"$WORKSPACE_DIR/transcoder-bin" server coordinator --config "$WORKSPACE_DIR/configs/us-east.yaml" --region us-east > "$LOGS_DIR/us-east-coordinator.log" 2>&1 &
"$WORKSPACE_DIR/transcoder-bin" server worker --config "$WORKSPACE_DIR/configs/us-east.yaml" --region us-east > "$LOGS_DIR/us-east-worker.log" 2>&1 &

# --- Region B: EU-West ---
echo "     👉 Booting EU-West Nodes..."
"$WORKSPACE_DIR/transcoder-bin" server gateway --config "$WORKSPACE_DIR/configs/eu-west.yaml" --region eu-west > "$LOGS_DIR/eu-west-gateway.log" 2>&1 &
"$WORKSPACE_DIR/transcoder-bin" server coordinator --config "$WORKSPACE_DIR/configs/eu-west.yaml" --region eu-west > "$LOGS_DIR/eu-west-coordinator.log" 2>&1 &
"$WORKSPACE_DIR/transcoder-bin" server worker --config "$WORKSPACE_DIR/configs/eu-west.yaml" --region eu-west > "$LOGS_DIR/eu-west-worker.log" 2>&1 &

# 6. Start CRR Manifest Replication Simulator
echo "🔄 4. Starting Cross-Region Replication (CRR) Simulator..."
go run "$WORKSPACE_DIR/scripts/simulate_crr.go" > "$LOGS_DIR/simulate-crr.log" 2>&1 &

echo "⏳ Allowing 3 seconds for all daemons to establish network listeners..."
sleep 3

echo "======================================================================"
echo " 🎉 SIMULATION RUNNING SUCCESSFULLY!"
echo "======================================================================"
echo "Log files are stored in: $LOGS_DIR"
echo "  - US-East Gateway Log:     tail -f logs/us-east-gateway.log"
echo "  - US-East Coordinator Log: tail -f logs/us-east-coordinator.log"
echo "  - US-East Worker Log:      tail -f logs/us-east-worker.log"
echo "  - EU-West Worker Log:      tail -f logs/eu-west-worker.log"
echo "  - S3 CRR Sync Log:         tail -f logs/simulate-crr.log"
echo "======================================================================"
echo "👉 How to test regional flow:"
echo ""
echo "1. Upload a video through the US-East Gateway (Port 8080):"
echo "   curl -X POST http://127.0.0.1:8080/api/jobs/upload-session \\"
echo "        -H 'Content-Type: application/json' \\"
echo "        -d '{\"file_size_bytes\": 1048576, \"file_name\": \"test.mp4\", \"content_type\": \"video/mp4\"}'"
echo ""
echo "2. Watch the logs. Note how US-East Worker logs transcoding tasks,"
echo "   while EU-West Worker logs remain completely idle (Job Isolation)."
echo ""
echo "3. Watch the S3 CRR logs to see manifests replication to EU-West bucket."
echo "======================================================================"
echo "Press Ctrl+C to terminate simulation and tear down container services."
echo "======================================================================"

# Keep script running to block and display stdout/logs dynamically
wait
