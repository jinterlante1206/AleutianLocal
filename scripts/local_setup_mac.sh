#!/bin/bash

echo "Starting Aleutian Local Setup for macOS..."

# --- Dependency Checks ---
echo "Checking dependencies..."
if ! command -v brew &> /dev/null; then
    echo "Homebrew not found. Please install it first: https://brew.sh/"
    exit 1
fi

if ! command -v podman &> /dev/null; then
    echo "Podman not found. Attempting to install via Homebrew..."
    brew install podman podman-compose podman-desktop || { echo "Podman installation failed."; exit 1; }
    echo "Podman installed. Please ensure Podman Desktop is running."
    # Instruct user to start Podman Desktop manually as it might require GUI interaction
    read -p "Press Enter once Podman Desktop is running..."
else
    echo "Podman found."
fi

# --- Directory Setup ---
echo "Creating necessary directories..."
mkdir -p ./models
# mkdir -p ./data # For Weaviate, if using bind mounts instead of named volumes

# --- Image Pulling ---
echo "Pulling required container images (this might take a while)..."
# Extract image names directly from podman-compose.yml to avoid duplication
IMAGES=$(grep 'image:' podman-compose.yml | sed 's/.*image: //')
for img in $IMAGES; do
    echo "Pulling $img..."
    podman pull "$img" || { echo "Failed to pull image: $img"; exit 1; }
done
echo "All images pulled."

# --- Configuration ---
if [ ! -f config.yaml ]; then
    echo "No config.yaml found. Copying community template..."
    cp config/community.yaml config.yaml
    echo "config.yaml created. Please review and edit if necessary (e.g., model paths)."
else
    echo "Existing config.yaml found."
fi

# --- Initial Startup ---
echo "Performing initial service startup..."
podman-compose up -d || { echo "❌ Failed to start services."; exit 1; }

echo "🎉 Aleutian Local Setup Complete!"
echo "Your AI appliance is running."
echo "Use './aleutian local stop' to stop it or './aleutian local destroy' to remove everything."
echo "Access the orchestrator at http://localhost:12210 (or your configured port)."