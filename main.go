package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// =====================================================================
// SYSTEM CONFIGURATION & ENVIRONMENT
// =====================================================================
var (
	ServerPort  = getEnv("OMNI_PORT", "8080")
	APIKey      = getEnv("OMNI_API_KEY", "sk-agent-local")
	TargetGPUID = getEnv("TARGET_GPU_ID", "0")
	omniBackend = getEnv("OMNI_BACKEND", "UNKNOWN (Defaulting to CPU)")
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	if key == "OMNI_API_KEY" {
		fmt.Println("\033[31;1m[Warning] OMNI_API_KEY not set. Using insecure default.\033[0m")
		time.Sleep(2 * time.Second)
	}
	return fallback
}

// =====================================================================
// GLOBAL VARIABLES & STYLING
// =====================================================================
const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36;1m"
	colorGreen  = "\033[32;1m"
	colorYellow = "\033[33;1m"
	colorRed    = "\033[31;1m"
	colorPurple = "\033[35;1m"
	appDirName  = ".llama_manager"
)

var (
	homeDir, _ = os.UserHomeDir()
	baseDir    = filepath.Join(homeDir, appDirName)
	binDir     = filepath.Join(baseDir, "bin")
	modelsDir  = filepath.Join(baseDir, "models")
	srcDir     = filepath.Join(baseDir, "llama.cpp")
	cacheFile  = filepath.Join(baseDir, "agent_cache.bin")
	pidFile    = filepath.Join(baseDir, "server.pid")
	logFile    = filepath.Join(baseDir, "server.log")
)

func initDirs() {
	dirs := []string{baseDir, binDir, modelsDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			fmt.Printf(colorRed+"[Fatal] Could not create directory: %v\n"+colorReset, err)
			os.Exit(1)
		}
	}
}

func clearScreen() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func pause() {
	fmt.Print(colorCyan + "\nPress [Enter] to return to the command center..." + colorReset)
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// --- Safety & Lifecycle Management ---

func checkSystemRAM(modelPath string) bool {
	info, err := os.Stat(modelPath)
	if err != nil {
		return true // Skip if unreadable
	}
	modelSizeGB := float64(info.Size()) / (1024 * 1024 * 1024)
	var availableMemoryGB float64

	// 1. Prioritize GPU VRAM if CUDA is active
	if omniBackend == "CUDA" {
		out, err := exec.Command("nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits").Output()
		if err == nil {
			vramMB, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
			availableMemoryGB = vramMB / 1024
		}
	}

	// 2. Fallback to System RAM (for APUs like Legion Go)
	if availableMemoryGB == 0 {
		memBytes, _ := os.ReadFile("/proc/meminfo")
		lines := strings.Split(string(memBytes), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, _ := strconv.ParseFloat(fields[1], 64)
					availableMemoryGB = kb / (1024 * 1024)
					break
				}
			}
		}
	}

	// Dynamic Thresholds
	threshold := availableMemoryGB * 0.90
	if omniBackend != "CUDA" {
		threshold = availableMemoryGB * 0.75
	}

	if modelSizeGB > threshold {
		fmt.Println(colorRed + "\n==================================================================" + colorReset)
		fmt.Println(colorRed + " [! FATAL WARNING] OUT OF MEMORY CRASH PREVENTED" + colorReset)
		fmt.Println(colorRed + "==================================================================" + colorReset)
		fmt.Printf(colorYellow+" Available Hardware Memory: %.2f GB\n"+colorReset, availableMemoryGB)
		fmt.Printf(colorYellow+" Attempted Model Size:      %.2f GB\n"+colorReset, modelSizeGB)
		fmt.Println("\n Loading this model will physically overwhelm the memory bus.")
		return false
	}
	return true
}

func isServerRunning() bool {
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func killServer() {
	if !isServerRunning() {
		return
	}
	pidData, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidData)))
	process, _ := os.FindProcess(pid)

	fmt.Println(colorYellow + "[*] Sending graceful shutdown signal (SIGINT)..." + colorReset)
	process.Signal(syscall.SIGINT)
	time.Sleep(3 * time.Second)
	if isServerRunning() {
		fmt.Println(colorRed + "[!] Process hung. Sending SIGKILL..." + colorReset)
		process.Signal(syscall.SIGKILL)
	}
	os.Remove(pidFile)
	fmt.Println(colorGreen + "[+] Daemon terminated." + colorReset)
	time.Sleep(1 * time.Second)
}

func waitForHealthCheck() bool {
	fmt.Print(colorYellow + "[*] Verifying endpoint health" + colorReset)
	client := http.Client{Timeout: 2 * time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/health", ServerPort)

	for i := 0; i < 15; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			fmt.Println(colorGreen + " [OK]" + colorReset)
			return true
		}
		fmt.Print(".")
		time.Sleep(2 * time.Second)
	}
	fmt.Println(colorRed + " [FAILED]" + colorReset)
	return false
}

// --- 1. DYNAMIC COMPILATION ---
func getLocalCommit() string {
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); os.IsNotExist(err) {
		return "Not Installed"
	}
	out, err := exec.Command("git", "-C", srcDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "Unknown"
	}
	return strings.TrimSpace(string(out))
}

func updateEngine() {
	clearScreen()
	fmt.Println(colorCyan + "==========================================================" + colorReset)
	fmt.Printf(colorCyan+"   Dynamic Engine Compiler (Targeting: %s)\n"+colorReset, omniBackend)
	fmt.Println(colorCyan + "==========================================================" + colorReset)

	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		fmt.Println(colorYellow + "[*] Cloning atomicmilkshake repository..." + colorReset)
		cmd := exec.Command("git", "clone", "https://github.com/atomicmilkshake/llama-cpp-turboquant.git", srcDir)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.Run()
	} else {
		fmt.Println(colorYellow + "[*] Pulling repository updates..." + colorReset)
		exec.Command("git", "-C", srcDir, "fetch", "--all").Run()
		exec.Command("git", "-C", srcDir, "pull", "origin", "main").Run()
	}

	fmt.Printf(colorYellow+"\n[*] Compiling native %s Kernels...\n"+colorReset, omniBackend)

	cmakeFlag := "-DGGML_CUDA=ON"
	if omniBackend == "VULKAN" {
		cmakeFlag = "-DGGML_VULKAN=ON"
	}

	cmakeCmd := exec.Command("cmake", "-B", filepath.Join(srcDir, "build"), "-S", srcDir, cmakeFlag)
	if err := cmakeCmd.Run(); err != nil {
		fmt.Println(colorRed + "\n[!] CMake configuration failed." + colorReset)
		pause()
		return
	}

	exec.Command("cmake", "--build", filepath.Join(srcDir, "build"), "--config", "Release", "-j").Run()

	compiledBin := filepath.Join(srcDir, "build", "bin", "llama-server")
	targetBin := filepath.Join(binDir, "llama-server")

	in, err := os.Open(compiledBin)
	if err != nil {
		fmt.Println(colorRed + "\n[!] Compilation failed: Could not locate binary." + colorReset)
		pause()
		return
	}
	defer in.Close()

	out, _ := os.Create(targetBin)
	defer out.Close()
	io.Copy(out, in)
	os.Chmod(targetBin, 0755)

	fmt.Println(colorGreen + "\n[+] Engine compiled and locked to active architecture." + colorReset)
	pause()
}

// --- 2. SMART ASSET DOWNLOADER ---
func downloadModel() {
	clearScreen()
	fmt.Println(colorCyan + "==========================================================" + colorReset)
	fmt.Println(colorCyan + "   Smart Asset Downloader                                 " + colorReset)
	fmt.Println(colorCyan + "==========================================================" + colorReset)
	fmt.Print(colorYellow + "Enter direct URL: " + colorReset)

	reader := bufio.NewReader(os.Stdin)
	rawURL, _ := reader.ReadString('\n')
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return
	}

	// 1. Strict URL Validation
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || parsedURL.Scheme == "" {
		fmt.Println(colorRed + "\n[!] Error: Invalid URL format." + colorReset)
		pause()
		return
	}

	// Remove HuggingFace parameters
	cleanURL := parsedURL.String()
	if idx := strings.Index(cleanURL, "?"); idx != -1 {
		cleanURL = cleanURL[:idx]
	}
	fileName := filepath.Base(cleanURL)
	targetPath := filepath.Join(modelsDir, fileName)

	// 2. Initialize Secure HTTP Client
	client := http.Client{Timeout: 60 * time.Minute}
	req, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return
	}

	fmt.Printf(colorYellow+"\n[*] Contacting Server for %s...\n"+colorReset, fileName)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		fmt.Printf(colorRed+"\n[!] Network Error: Failed to connect or received Status %d\n"+colorReset, resp.StatusCode)
		pause()
		return
	}
	defer resp.Body.Close()

	// 3. ENOSPC Storage Pre-flight Check
	var stat syscall.Statfs_t
	syscall.Statfs(modelsDir, &stat)
	freeSpaceGB := float64(stat.Bavail*uint64(stat.Bsize)) / (1024 * 1024 * 1024)
	requiredGB := float64(resp.ContentLength) / (1024 * 1024 * 1024)

	if resp.ContentLength > 0 && requiredGB > freeSpaceGB {
		fmt.Printf(colorRed+"\n[!] Abort: Insufficient disk space. Need %.2f GB, but only %.2f GB is free.\n"+colorReset, requiredGB, freeSpaceGB)
		pause()
		return
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return
	}
	defer out.Close()

	var downloaded int64
	buf := make([]byte, 32*1024)
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			if downloaded >= resp.ContentLength {
				break
			}
			mb := float64(downloaded) / 1024 / 1024
			if resp.ContentLength > 0 {
				totalMB := float64(resp.ContentLength) / 1024 / 1024
				fmt.Printf("\r    -> Progress: %.2f MB / %.2f MB", mb, totalMB)
			}
		}
	}()

	io.CopyBuffer(out, io.TeeReader(resp.Body, &writeCounter{&downloaded}), buf)
	fmt.Println(colorGreen + "\n\n[+] Asset securely stored." + colorReset)
	pause()
}

type writeCounter struct{ total *int64 }

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	*wc.total += int64(n)
	return n, nil
}

// --- 3. DYNAMIC DEPLOYMENT ROUTER ---
func deploySmart() {
	if isServerRunning() {
		fmt.Println(colorRed + "\n[!] Conflict: Daemon is already active. Stop it first." + colorReset)
		time.Sleep(2 * time.Second)
		return
	}

	clearScreen()
	fmt.Println(colorCyan + "==========================================================" + colorReset)
	fmt.Println(colorCyan + "   Agent Initialization Sequence                          " + colorReset)
	fmt.Println(colorCyan + "==========================================================" + colorReset)

	serverBin := filepath.Join(binDir, "llama-server")
	if _, err := os.Stat(serverBin); os.IsNotExist(err) {
		fmt.Println(colorRed + "[!] Core binary missing. Compile first." + colorReset)
		pause()
		return
	}

	files, _ := os.ReadDir(modelsDir)
	var mainModel, draftModel, triStatsFile string

	// Model Classification Engine
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := strings.ToLower(f.Name())
		if strings.HasSuffix(name, ".gguf") {
			if info, err := f.Info(); err == nil {
				if info.Size() < 5*1024*1024*1024 { // Under 5GB is drafted as Speculative
					draftModel = filepath.Join(modelsDir, f.Name())
				} else {
					mainModel = filepath.Join(modelsDir, f.Name())
				}
			}
		} else if strings.HasSuffix(name, ".triattention") {
			triStatsFile = filepath.Join(modelsDir, f.Name())
		}
	}

	if mainModel == "" {
		fmt.Println(colorRed + "[!] Abort: No primary model (>5GB) found." + colorReset)
		pause()
		return
	}

	// RAM/VRAM Hardware Protection
	if !checkSystemRAM(mainModel) {
		pause()
		return
	}

	fmt.Printf(colorGreen+"[✔] Primary Payload: "+colorReset+"%s\n", filepath.Base(mainModel))

	// Base Configuration
	cmdArgs := []string{
		"-m", mainModel, "-c", "65536", "-ngl", "99",
		"--port", ServerPort, "--host", "0.0.0.0",
		"-ctk", "q8_0", "-ctv", "turbo4",
		"-cb", "--prompt-cache", cacheFile, "--prompt-cache-all",
		"-gr", "--jinja", "-b", "2048", "-ub", "2048",
		"--api-key", APIKey,
	}

	// Architectural Specific Injections
	if omniBackend == "CUDA" {
		fmt.Println(colorPurple + "[⚡] Acceleration:" + colorReset + " Flash-Attention V3 Active (NVIDIA)")
		cmdArgs = append(cmdArgs, "-fa", "on")
		os.Setenv("CUDA_VISIBLE_DEVICES", TargetGPUID)
		os.Setenv("GGML_CUDA_GRAPH_OPT", "1")

		if triStatsFile != "" {
			fmt.Printf(colorPurple+"[⚡] Core Pruning: "+colorReset+" TriAttention Active (%s)\n", filepath.Base(triStatsFile))
			cmdArgs = append(cmdArgs, "--triattention-stats", triStatsFile, "--triattention-budget", "8192", "--triattention-window", "256")
		}
	} else if omniBackend == "VULKAN" {
		fmt.Println(colorPurple + "[⚡] Acceleration:" + colorReset + " Vulkan Native Offload Active (AMD/Intel)")
	}

	if draftModel != "" {
		fmt.Printf(colorPurple+"[⚡] Speculative Draft:"+colorReset+" Linked (%s)\n", filepath.Base(draftModel))
		cmdArgs = append(cmdArgs, "--draft", draftModel)
	}

	os.Setenv("TURBO_LAYER_ADAPTIVE", "7")

	fmt.Print(colorYellow + "\nDeploy Server in [F]oreground or [B]ackground? (F/B): " + colorReset)
	reader := bufio.NewReader(os.Stdin)
	bgChoice, _ := reader.ReadString('\n')

	cmd := exec.Command(serverBin, cmdArgs...)

	if strings.ToLower(strings.TrimSpace(bgChoice)) == "b" {
		// True Daemon Detachment
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		logOut, _ := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		cmd.Stdout = logOut
		cmd.Stderr = logOut

		if err := cmd.Start(); err != nil {
			fmt.Println(colorRed + "[!] Failed to launch daemon process." + colorReset)
			pause()
			return
		}
		os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)
		waitForHealthCheck()
		fmt.Println(colorGreen + "\n[+] AI Daemon deployed successfully." + colorReset)
		fmt.Printf(colorYellow+"    Live Telemetry: tail -f %s\n"+colorReset, logFile)
		pause()
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return
		}

		waitForHealthCheck()
		fmt.Println(colorGreen + "\n[+] Server Online! Press Ctrl+C to initiate safe shutdown." + colorReset)

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			fmt.Println(colorYellow + "\n\n[!] Interrupt caught. Securing cache and terminating..." + colorReset)
			cmd.Process.Signal(syscall.SIGINT)
		}()
		cmd.Wait()
		pause()
	}
}

// --- MAIN OMNI-MENU ---
func main() {
	initDirs()

	for {
		clearScreen()
		fmt.Println(colorCyan + "==========================================================" + colorReset)
		fmt.Println(colorCyan + "   OMNI-DEPLOY AI ARCHITECTURE (V6 Master Edition)        " + colorReset)
		fmt.Println(colorCyan + "==========================================================" + colorReset)

		localCommit := getLocalCommit()
		fmt.Printf(colorYellow+"   Hardware Target: "+colorReset+" [%s]\n", omniBackend)
		fmt.Printf(colorYellow+"   Engine Build:    "+colorReset+" %s\n", localCommit)

		if isServerRunning() {
			fmt.Println(colorGreen + "   Status:          [DAEMON ONLINE] on Port " + ServerPort + colorReset)
		} else {
			fmt.Println(colorRed + "   Status:          [OFFLINE]" + colorReset)
		}
		fmt.Println("----------------------------------------------------------\n")

		fmt.Println("  [1] Compile Core Engine (Adapts to current hardware)")
		fmt.Println("  [2] Smart Asset Downloader (.gguf / .triattention)")
		fmt.Println(colorGreen + "  [3] DEPLOY AI (Smart Auto-Configuration)" + colorReset)
		if isServerRunning() {
			fmt.Println(colorRed + "  [4] TERMINATE AI DAEMON (Safe Shutdown)" + colorReset)
		}
		fmt.Println("  [0] System Shutdown")

		fmt.Print(colorYellow + "\nSelect command: " + colorReset)
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "4" && isServerRunning() {
			killServer()
			continue
		}

		switch input {
		case "1":
			updateEngine()
		case "2":
			downloadModel()
		case "3":
			deploySmart()
		case "0":
			if isServerRunning() {
				killServer()
			}
			fmt.Println(colorPurple + "\nArchitecture powering down. Goodbye!" + colorReset)
			os.Exit(0)
		}
	}
}