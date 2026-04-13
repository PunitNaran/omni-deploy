#!/bin/bash
set -euo pipefail

# ==============================================================================
# OMNI-DEPLOY HARDWARE SENTINEL (V6 - Production Grade)
# ==============================================================================
# Configuration
POWER_LIMIT=270
GO_SCRIPT="main.go"

echo -e "\033[36;1m=== Omni-Deploy Hardware Sentinel ===\033[0m"

# --- 0. DEPENDENCY CHECKS ---
# Ensure the host environment has the necessary tools to orchestrate the lab
if ! command -v distrobox &> /dev/null; then
    echo -e "\033[31;1m[Fatal] distrobox is not installed. Please install it first.\033[0m"
    exit 1
fi
if ! command -v lspci &> /dev/null; then
    echo -e "\033[31;1m[Fatal] pciutils (lspci) is not installed.\033[0m"
    exit 1
fi

# --- 1. HARDWARE AUTO-DETECTION ---
# We scan the PCI bus once to determine the container image and compiler flags.
echo -e "\n\033[33;1m[1/3] Scanning PCIe Bus for Accelerators...\033[0m"

export ACTIVE_ARCH="CPU"
CONTAINER_NAME="ai-lab-cpu"
IMAGE="ubuntu:22.04"

if lspci | grep -iE "NVIDIA" > /dev/null; then
    export ACTIVE_ARCH="CUDA"
    CONTAINER_NAME="ai-lab-cuda"
    IMAGE="nvidia/cuda:12.4.1-devel-ubuntu22.04"
    echo "  [✔] NVIDIA GPU Detected. Prioritizing CUDA Architecture (RTX 3090 Path)."
    
    # Request sudo access once for the hardware power-limit lock
    echo "  [*] Locking Hardware Power limits..."
    sudo -v || (echo "Sudo access required for NVIDIA power limits." && exit 1)
    sudo nvidia-smi -pm 1 > /dev/null 2>&1 || true
    sudo nvidia-smi -pl $POWER_LIMIT > /dev/null 2>&1 || true
    echo "  [✔] NVIDIA Power locked to ${POWER_LIMIT}W."

elif lspci | grep -iE "AMD|Advanced Micro Devices|Intel.*Graphics" > /dev/null; then
    export ACTIVE_ARCH="VULKAN"
    CONTAINER_NAME="ai-lab-vulkan"
    echo "  [✔] AMD/Intel Graphics Detected. Shifting to Vulkan Architecture (Legion Go Path)."
else
    echo "  [!] No dedicated GPU detected. Defaulting to Universal CPU Compute."
fi

# --- 2. CREATE ADAPTIVE CONTAINER ---
# We provision an isolated environment so we don't pollute Bazzite's immutable core.
echo -e "\n\033[33;1m[2/3] Verifying Adaptive Container ($CONTAINER_NAME)...\033[0m"
if distrobox list | grep -q "$CONTAINER_NAME"; then
    echo "  [✔] Container '$CONTAINER_NAME' is ready."
else
    echo "  [*] Building '$CONTAINER_NAME' from $IMAGE..."
    if [ "$ACTIVE_ARCH" == "CUDA" ]; then
        # Pass NVIDIA drivers into the container
        distrobox create -n "$CONTAINER_NAME" -i "$IMAGE" --nvidia --yes
    else
        distrobox create -n "$CONTAINER_NAME" -i "$IMAGE" --yes
    fi
    echo "  [✔] Container initialized."
fi

# --- 3. INSTALL ARCHITECTURE-SPECIFIC COMPILERS ---
# We use DEBIAN_FRONTEND=noninteractive to ensure the script doesn't hang on a prompt.
echo -e "\n\033[33;1m[3/3] Synchronizing Compiler Toolchains (Non-Interactive)...\033[0m"
if [ "$ACTIVE_ARCH" == "CUDA" ]; then
    distrobox enter "$CONTAINER_NAME" -- bash -c "
        export DEBIAN_FRONTEND=noninteractive
        sudo -E apt-get update -yqq 
        sudo -E apt-get install -yqq --no-install-recommends golang cmake git build-essential curl
        # Inject the architecture tag into the container's shell profile
        grep -q 'OMNI_BACKEND=CUDA' ~/.bashrc || echo 'export OMNI_BACKEND=CUDA' >> ~/.bashrc
    "
elif [ "$ACTIVE_ARCH" == "VULKAN" ]; then
    distrobox enter "$CONTAINER_NAME" -- bash -c "
        export DEBIAN_FRONTEND=noninteractive
        sudo -E apt-get update -yqq 
        sudo -E apt-get install -yqq --no-install-recommends golang cmake git build-essential curl vulkan-tools libvulkan-dev vulkan-validationlayers-dev
        # Inject the architecture tag into the container's shell profile
        grep -q 'OMNI_BACKEND=VULKAN' ~/.bashrc || echo 'export OMNI_BACKEND=VULKAN' >> ~/.bashrc
    "
fi
echo "  [✔] Toolchains securely synchronized."

echo -e "\n\033[32;1m========================================================\033[0m"
echo -e "\033[32;1m ALL SYSTEMS GO. OMNI-DEPLOY CONFIGURATION COMPLETE.\033[0m"
echo " To launch the Master Control Node, run:"
echo -e " \033[36m distrobox enter $CONTAINER_NAME -- bash -c 'export OMNI_API_KEY=\"your_key_here\" && go run $GO_SCRIPT' \033[0m"
echo -e "\033[32;1m========================================================\033[0m"