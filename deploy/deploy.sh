#!/bin/bash

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SERVER_IP="138.197.13.3"
SERVER_USER="root"
REMOTE_DIR="/root/octree-compile"
SERVICE_NAME="octree-compile"
DOCKER_IMAGE="octree-compile:latest"

echo -e "${GREEN}🚀 Starting deployment to $SERVER_IP${NC}"

# Step 1: Sync code to server
echo -e "${YELLOW}📦 Syncing code to server...${NC}"
rsync -avz --delete \
  --exclude 'logs/' \
  --exclude 'latex-compile' \
  --exclude '.git/' \
  --exclude 'node_modules/' \
  ./ $SERVER_USER@$SERVER_IP:$REMOTE_DIR/

# Step 2: Deploy on server
echo -e "${YELLOW}🔧 Deploying on server...${NC}"
ssh $SERVER_USER@$SERVER_IP << 'ENDSSH'
set -e

cd /root/octree-compile

echo "🛑 Stopping existing containers..."
docker-compose -f deployments/docker-compose.yml down || true

echo "🏗️  Building Docker image..."
docker-compose -f deployments/docker-compose.yml build --no-cache

echo "🚀 Starting services..."
docker-compose -f deployments/docker-compose.yml up -d

echo "⏳ Waiting for service to be ready..."
sleep 5

echo "✅ Checking service status..."
docker-compose -f deployments/docker-compose.yml ps

echo "📊 Container logs:"
docker-compose -f deployments/docker-compose.yml logs --tail=20

echo "🧪 Testing health endpoint..."
curl -s http://localhost:3001/health || echo "Health check failed"

ENDSSH

# Step 3: Test the deployment
echo -e "${YELLOW}🧪 Testing deployment...${NC}"
sleep 2
if ssh $SERVER_USER@$SERVER_IP "curl -s http://localhost:3001/health" | grep -q "ok"; then
    echo -e "${GREEN}✅ Deployment successful!${NC}"
    echo -e "${GREEN}Service is running at: http://$SERVER_IP:3001${NC}"
else
    echo -e "${RED}❌ Deployment may have issues. Check logs.${NC}"
    exit 1
fi

# Step 4: Show useful commands
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}Deployment Complete!${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo ""
echo "📝 Useful commands:"
echo "  View logs:    ssh $SERVER_USER@$SERVER_IP 'cd $REMOTE_DIR && docker-compose -f deployments/docker-compose.yml logs -f'"
echo "  Restart:      ssh $SERVER_USER@$SERVER_IP 'cd $REMOTE_DIR && docker-compose -f deployments/docker-compose.yml restart'"
echo "  Stop:         ssh $SERVER_USER@$SERVER_IP 'cd $REMOTE_DIR && docker-compose -f deployments/docker-compose.yml down'"
echo "  Shell access: ssh $SERVER_USER@$SERVER_IP 'cd $REMOTE_DIR && docker-compose -f deployments/docker-compose.yml exec latex-compile /bin/sh'"
echo ""
echo "🌐 API Endpoints:"
echo "  Health: http://$SERVER_IP:3001/health"
echo "  Compile: http://$SERVER_IP:3001/compile"
echo ""

