package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// CollectMetadata inspects the running host environment for display in the controller UI.
func CollectMetadata() map[string]string {
	meta := make(map[string]string)

	meta["os"] = fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	meta["cpus"] = fmt.Sprintf("%d cores", runtime.NumCPU())

	if cwd, err := os.Getwd(); err == nil {
		meta["cwd"] = cwd
	}

	// Detect Kaggle environment
	isKaggle := false
	if _, err := os.Stat("/kaggle"); err == nil {
		isKaggle = true
	}
	if os.Getenv("KAGGLE_KERNEL_RUN_TYPE") != "" || os.Getenv("KAGGLE_CONTAINER_NAME") != "" {
		isKaggle = true
	}

	if isKaggle {
		meta["environment"] = "Kaggle Notebook"
		if kernelType := os.Getenv("KAGGLE_KERNEL_RUN_TYPE"); kernelType != "" {
			meta["kaggle_run_type"] = kernelType
		}
	} else {
		meta["environment"] = "Linux Instance"
	}

	// Detect GPU
	gpu := detectGPU()
	if gpu != "" {
		meta["gpu"] = gpu
	} else {
		meta["gpu"] = "None (CPU only)"
	}

	return meta
}

func detectGPU() string {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return ""
	}

	cmd := exec.Command("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return ""
	}

	// If multiple GPUs, format cleanly
	if len(lines) == 1 {
		return strings.TrimSpace(lines[0])
	}
	return fmt.Sprintf("%dx %s", len(lines), strings.TrimSpace(lines[0]))
}
