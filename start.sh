#!/usr/bin/env bash
set -e

echo "============================================================"
echo " Starting Distributed Video Transcoding Engine (Platform)   "
echo "============================================================"

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "[ERROR] Docker is not installed. Please install Docker first."
    exit 1
fi

echo "[1/3] Building Go Engine and pulling infrastructure images..."
# We use docker compose build to build the Gateway, Coordinator, and Worker from the Dockerfile
docker compose -f docker-compose.prod.yml build

echo "[2/3] Starting the cluster..."
# Start all services in the background. Note: --scale worker=2 is set in the compose file natively
docker compose -f docker-compose.prod.yml --profile infra-selfhosted --profile backend up -d

echo "[3/3] Checking service health..."
sleep 5
docker compose -f docker-compose.prod.yml ps

echo "============================================================"
echo " Successfully started! "
echo "============================================================"
echo " Useful Endpoints:"
echo " - Gateway API & WebSockets : http://localhost:8080"
echo " - MinIO S3 Console         : http://localhost:9001 (minioadmin / minioadmin)"
echo ""
echo " To scale the transcoder workers on the fly, run:"
echo " docker compose -f docker-compose.prod.yml up -d --scale worker=5"
echo ""
echo " To view logs:"
echo " docker compose -f docker-compose.prod.yml logs -f"
echo "============================================================"
