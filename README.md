# Omni-Deploy AI Architecture 🚀
**Platinum Plus Production Grade v6.0**

Omni-Deploy is a hardware-aware, self-compiling deployment engine for Large Language Models (LLMs). It is designed to bridge the gap between high-end eGPU setups (NVIDIA RTX 3090) and portable handhelds (Lenovo Legion Go / AMD APUs) by dynamically optimizing the inference stack based on the detected hardware.

---

## 🌟 Key Features

* **Omni-Hardware Sentinel:** Scans the PCIe bus on boot to distinguish between NVIDIA CUDA, AMD/Intel Vulkan, or pure CPU environments.
* **Intelligent Memory Protection:** A pre-flight logic gate that calculates model size against available VRAM (NVIDIA) or System RAM (AMD APU) to prevent hard system freezes.
* **Atomic Pruning & Compression:** Integrated support for **TurboQuant (4-bit asymmetric KV)** and **TriAttention (Trigonometric KV Pruning)** for up to 50x memory efficiency.
* **Production Lifecycle Management:** True background daemonization using Unix `Setsid`, PID tracking, and graceful `SIGINT` cache-saving shutdowns.
* **Speculative Decoding:** Automatically detects and links draft models (e.g., 1.5B) to main models (e.g., 35B) for a ~2x speed boost.
* **Smart Downloader:** Sanitizes HuggingFace URLs and performs disk-space validation (`ENOSPC` check) before committing to 20GB+ downloads.

---

## 🛠️ Prerequisites

* **Host OS:** Linux (Optimized for Bazzite/Fedora/Ubuntu).
* **Orchestration:** `distrobox` and `podman/docker` must be installed.
* **Drivers:** Hardware drivers (NVIDIA/AMD) should be active on the host.

---

## 🚀 Installation & First Run

### 1. Initialize the Host
Run the sentinel script on your Bazzite/Linux host to lock GPU power limits and provision the optimized container:
```bash
chmod +x setup_omni.sh
./setup_omni.sh
```

### 2. Enter the Environment
The setup script will create a specific container based on your hardware (`ai-lab-cuda` or `ai-lab-vulkan`). Enter it via:
```bash
distrobox enter ai-lab-cuda  # Or the name provided by the setup script
```

### 3. Launch the Control Node
Inside the container, set your secure API key and launch the manager:
```bash
export OMNI_API_KEY="your-secret-key-here"
go run main.go
```

---

## 🕹️ Hardware Specifics

| Feature | NVIDIA (eGPU/3090) | AMD (Legion Go Internal) |
| :--- | :--- | :--- |
| **Backend** | CUDA 12.4 | Vulkan |
| **KV Compression** | TurboQuant 4-bit | TurboQuant 4-bit |
| **Memory Pruning** | TriAttention (CUDA) | Disabled (Stability) |
| **Acceleration** | Flash Attention V3 | Native Vulkan Offload |
| **Context Window** | Up to 128k (Pruned) | Hardware-Limited (16GB Shared) |

---

## 🛡️ Security & Ops

* **API Security:** The server defaults to `sk-agent-local`. Always set `OMNI_API_KEY` in your environment for production use.
* **Process Control:** Background servers are tracked via `server.pid`. Use **Option 4** in the menu to safely shut down the daemon and save the KV cache.
* **Telemetry:** Use `tail -f server.log` to monitor real-time inference performance and hardware temperatures.

---

## ⚖️ License
This project is provided for high-performance local AI research. Use responsibly.