#!/bin/bash
# =============================================================================
# Fight Club - Oracle Cloud Deployment Script
# =============================================================================
# Usage: ./scripts/deploy-oracle.sh [command]
# Commands: setup, deploy, update, logs, status, restart, stop
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
APP_NAME="fight-club"
CONTAINER_NAME="fight-club-game-server"
DOCKER_COMPOSE_FILE="docker-compose.yml"

# Print colored message
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Check if running on ARM64
check_architecture() {
    ARCH=$(uname -m)
    if [ "$ARCH" != "aarch64" ] && [ "$ARCH" != "arm64" ]; then
        warn "Not running on ARM64 architecture (detected: $ARCH)"
        warn "This script is optimized for Oracle Cloud ARM instances"
    else
        log "Running on ARM64 architecture ✓"
    fi
}

# Check dependencies
check_dependencies() {
    log "Checking dependencies..."

    if ! command -v docker &> /dev/null; then
        error "Docker is not installed. Run: ./scripts/deploy-oracle.sh setup"
    fi

    if ! docker compose version &> /dev/null; then
        error "Docker Compose is not installed"
    fi

    log "All dependencies installed ✓"
}

# Install Docker on Ubuntu (Oracle Cloud)
install_docker() {
    log "Installing Docker..."

    # Update system
    sudo apt update && sudo apt upgrade -y

    # Install dependencies
    sudo apt install -y apt-transport-https ca-certificates curl software-properties-common

    # Add Docker GPG key
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg

    # Add repository (ARM64)
    echo "deb [arch=arm64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

    # Install Docker
    sudo apt update
    sudo apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

    # Add user to docker group
    sudo usermod -aG docker $USER

    log "Docker installed successfully!"
    warn "Please log out and back in, or run: newgrp docker"
}

# Configure firewall
configure_firewall() {
    log "Configuring firewall..."

    # Ubuntu uses iptables by default on Oracle Cloud
    sudo iptables -I INPUT -p tcp --dport 3000 -j ACCEPT
    sudo iptables -I INPUT -p tcp --dport 80 -j ACCEPT
    sudo iptables -I INPUT -p tcp --dport 443 -j ACCEPT

    # Save iptables rules
    if command -v netfilter-persistent &> /dev/null; then
        sudo netfilter-persistent save
    else
        sudo apt install -y iptables-persistent
        sudo netfilter-persistent save
    fi

    log "Firewall configured ✓"
}

# Setup environment
setup() {
    log "Setting up Oracle Cloud environment..."

    check_architecture

    # Install Docker if not present
    if ! command -v docker &> /dev/null; then
        install_docker
    else
        log "Docker already installed ✓"
    fi

    # Configure firewall
    configure_firewall

    # Create .env if not exists
    if [ ! -f ".env" ]; then
        if [ -f ".env.example" ]; then
            cp .env.example .env
            warn "Created .env from .env.example - please edit with your credentials"
            warn "Run: nano .env"
        else
            error ".env.example not found"
        fi
    else
        log ".env file exists ✓"
    fi

    log "Setup complete! Run: ./scripts/deploy-oracle.sh deploy"
}

# Build and deploy
deploy() {
    log "Deploying Fight Club..."

    check_dependencies

    # Verify .env exists
    if [ ! -f ".env" ]; then
        error ".env file not found. Run setup first or create manually."
    fi

    # Stop existing containers
    log "Stopping existing containers..."
    docker compose down --remove-orphans 2>/dev/null || true

    # Build for current platform (ARM64 on Oracle)
    log "Building Docker image..."
    docker compose build --no-cache

    # Start containers
    log "Starting containers..."
    docker compose up -d

    # Wait for container to be healthy
    log "Waiting for container to be healthy..."
    sleep 10

    # Check status
    if docker compose ps | grep -q "running"; then
        log "Deployment successful! ✓"
        info "API: http://$(curl -s ifconfig.me):3000/api/state"
        info "Admin: http://$(curl -s ifconfig.me):3000/admin"
    else
        error "Deployment failed. Check logs: docker compose logs"
    fi
}

# Update deployment (git pull + rebuild)
update() {
    log "Updating Fight Club..."

    check_dependencies

    # Pull latest changes
    log "Pulling latest changes..."
    git pull origin main || git pull origin master || warn "Could not pull from git"

    # Rebuild and restart
    log "Rebuilding..."
    docker compose down
    docker compose build --no-cache
    docker compose up -d

    log "Update complete! ✓"
}

# Show logs
logs() {
    log "Showing logs (Ctrl+C to exit)..."
    docker compose logs -f
}

# Show status
status() {
    log "Container status:"
    docker compose ps

    echo ""
    log "Resource usage:"
    docker stats --no-stream

    echo ""
    log "Health check:"
    if curl -s http://localhost:3000/api/state > /dev/null; then
        echo -e "${GREEN}API is responding ✓${NC}"
    else
        echo -e "${RED}API is not responding ✗${NC}"
    fi
}

# Restart containers
restart() {
    log "Restarting containers..."
    docker compose restart
    log "Restart complete ✓"
}

# Stop containers
stop() {
    log "Stopping containers..."
    docker compose down
    log "Containers stopped ✓"
}

# Show help
help() {
    echo "Fight Club - Oracle Cloud Deployment Script"
    echo ""
    echo "Usage: ./scripts/deploy-oracle.sh [command]"
    echo ""
    echo "Commands:"
    echo "  setup     Install Docker and configure environment"
    echo "  deploy    Build and deploy the application"
    echo "  update    Pull latest code and redeploy"
    echo "  logs      Show container logs"
    echo "  status    Show container status and health"
    echo "  restart   Restart containers"
    echo "  stop      Stop containers"
    echo "  help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  ./scripts/deploy-oracle.sh setup   # First time setup"
    echo "  ./scripts/deploy-oracle.sh deploy  # Deploy application"
    echo "  ./scripts/deploy-oracle.sh update  # Update after git pull"
}

# Main
case "${1:-help}" in
    setup)
        setup
        ;;
    deploy)
        deploy
        ;;
    update)
        update
        ;;
    logs)
        logs
        ;;
    status)
        status
        ;;
    restart)
        restart
        ;;
    stop)
        stop
        ;;
    help|--help|-h)
        help
        ;;
    *)
        error "Unknown command: $1. Run: ./scripts/deploy-oracle.sh help"
        ;;
esac
