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

	// Detect GPU / TPU accelerators
	gpu := detectGPU()
	tpu := detectTPU()

	if tpu != "" {
		meta["gpu"] = tpu
		meta["accelerator"] = "TPU"
	} else if gpu != "" {
		meta["gpu"] = gpu
		meta["accelerator"] = "GPU"
	} else {
		meta["gpu"] = "None (CPU only)"
		meta["accelerator"] = "CPU"
	}

	return meta
}

func detectTPU() string {
	// 1. Check explicit TPU accelerator type environment variable
	if tpuType := os.Getenv("TPU_ACCELERATOR_TYPE"); tpuType != "" {
		return fmt.Sprintf("TPU %s (Google Cloud TPU)", tpuType)
	}

	// 2. Check Kaggle TPU version flag
	if tpuVer := os.Getenv("KAGGLE_TPU_VERSION"); tpuVer != "" {
		return fmt.Sprintf("Kaggle TPU v%s-8", tpuVer)
	}

	// 3. Check TPU_NAME
	if tpuName := os.Getenv("TPU_NAME"); tpuName != "" {
		if accel := os.Getenv("ACCELERATOR_TYPE"); accel != "" {
			return fmt.Sprintf("TPU %s (%s)", accel, tpuName)
		}
		return fmt.Sprintf("TPU (%s)", tpuName)
	}

	// 4. Check for libtpu runtime library
	for _, path := range []string{"/lib/libtpu.so", "/usr/local/lib/libtpu.so", "/usr/lib/libtpu.so"} {
		if _, err := os.Stat(path); err == nil {
			return "Google Cloud TPU VM"
		}
	}

	// 5. Check /dev/accel* device nodes
	if files, err := os.ReadDir("/dev"); err == nil {
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "accel") {
				return "Google Cloud TPU Device (/dev/" + f.Name() + ")"
			}
		}
	}

	return ""
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
