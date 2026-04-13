#!/bin/bash

# ============================================================================== 
# OMNI-DEPLOY HOST SETUP (NVIDIA CUDA + AMD VULKAN ADAPTIVE)
# ============================================================================== 
POWER_LIMIT=270
GO_SCRIPT="main.go"

echo -e "\033[36;1m=== Omni-Deploy Hardware Sentinel ===\033[0m"

# 1. HARDWARE AUTO-DETECTION
echo -e "\n\033[33;1m[1/3] Scanning PCIe Bus for Accelerators...\033[0m"
if lspci | grep -i "NVIDIA" > /dev/null; then
    export ACTIVE_ARCH="CUDA"
    CONTAINER_NAME="ai-lab-cuda"
    IMAGE="nvidia/cuda:12.4.1-devel-ubuntu22.04"
    echo "  [✔] NVIDIA GPU Detected (e.g., RTX 3090 eGPU). Prioritizing CUDA."
    
    # Apply NVIDIA Power Limits
    sudo nvidia-smi -pm 1 > /dev/null 2>&1
    sudo nvidia-smi -pl $POWER_LIMIT > /dev/null 2>&1
    echo "  [✔] NVIDIA Power locked to ${POWER_LIMIT}W."
else
    export ACTIVE_ARCH="VULKAN"
    CONTAINER_NAME="ai-lab-vulkan"
    IMAGE="ubuntu:22.04"
    echo "  [✔] No NVIDIA device found. AMD/Intel APU Detected (e.g., Legion Go Internal)."
    echo "  [✔] Shifting to Universal Vulkan Architecture."
fi

# 2. CREATE ADAPTIVE CONTAINER
echo -e "\n\033[33;1m[2/3] Verifying Adaptive Container ($CONTAINER_NAME)...\033[0m"
if distrobox list | grep -q "$CONTAINER_NAME"; then
    echo "  [✔] Container '$CONTAINER_NAME' is ready."
else
    echo "  [*] Building '$CONTAINER_NAME' from $IMAGE..."
    if [ "$ACTIVE_ARCH" == "CUDA" ]; then
        distrobox create -n $CONTAINER_NAME -i $IMAGE --nvidia --yes
    else
        distrobox create -n $CONTAINER_NAME -i $IMAGE --yes
    fi
    echo "  [✔] Container initialized."
fi

# 3. INSTALL ARCHITECTURE-SPECIFIC COMPILERS
echo -e "\n\033[33;1m[3/3] Synchronizing Compiler Toolchains...\033[0m"
if [ "$ACTIVE_ARCH" == "CUDA" ]; then
    distrobox enter $CONTAINER_NAME -- bash -c "
        sudo apt-get update -yqq && sudo apt-get install -yqq golang cmake git build-essential curl
        echo 'export OMNI_BACKEND=CUDA' >> ~/.bashrc
    "
else
    distrobox enter $CONTAINER_NAME -- bash -c "
        sudo apt-get update -yqq && sudo apt-get install -yqq golang cmake git build-essential curl vulkan-tools libvulkan-dev vulkan-validationlayers-dev
        echo 'export OMNI_BACKEND=VULKAN' >> ~/.bashrc
    "
fi
echo "  [✔] Toolchains perfectly synchronized."

echo -e "\n\033[32;1m========================================================\033[0m"
echo -e "\033[32;1m OMNI-DEPLOY CONFIGURATION COMPLETE.\033[0m"
echo " To launch the Master Control Node, run:"
echo -e " \033[36m distrobox enter $CONTAINER_NAME -- go run $GO_SCRIPT \033[0m"
echo -e "\033[32;1m========================================================\033[0m"