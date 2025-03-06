//go:build linux || windows

package discover

import (
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Jetson devices have JETSON_JETPACK="x.y.z" factory set to the Jetpack version installed.
// Included to drive logic for reducing Ollama-allocated overhead on L4T/Jetson devices.
var CudaTegra string = os.Getenv("JETSON_JETPACK")

// / cudaGetVisibleDevicesEnv constructs the CUDA_VISIBLE_DEVICES variable from the GPU info.
// If no CUDA devices are available (i.e. CPU-only mode), it returns an empty string.
func cudaGetVisibleDevicesEnv(gpuInfo []GpuInfo) (string, string) {
	// If no GPU info is available, return an empty string.
	if len(gpuInfo) == 0 {
		slog.Debug("cudaGetVisibleDevicesEnv: no GPU info available, returning empty string")
		return "CUDA_VISIBLE_DEVICES", ""
	}
	ids := []string{}
	for _, info := range gpuInfo {
		if info.Library != "cuda" {
			// TODO: shouldn't happen if things are wired correctly...
			slog.Debug("cudaGetVisibleDevicesEnv skipping over non-cuda device", "library", info.Library)
			continue
		}
		ids = append(ids, info.ID)
	}
	return "CUDA_VISIBLE_DEVICES", strings.Join(ids, ",")
}

// cudaVariant determines which CUDA variant to use for the given GPU.
// For ARM64 Linux devices (e.g., Jetson), it selects a Jetpack variant if available.
// For other architectures, it falls back to driver-based logic.
func cudaVariant(gpuInfo CudaGPUInfo) string {
	// If running on ARM64 Linux, check for Jetson-specific settings.
	if runtime.GOARCH == "arm64" && runtime.GOOS == "linux" {
		if CudaTegra != "" {
			ver := strings.Split(CudaTegra, ".")
			if len(ver) > 0 {
				return "jetpack" + ver[0]
			}
		} else if data, err := os.ReadFile("/etc/nv_tegra_release"); err == nil {
			r := regexp.MustCompile(` R(\d+) `)
			m := r.FindSubmatch(data)
			if len(m) != 2 {
				slog.Info("Unexpected format for /etc/nv_tegra_release.  Set JETSON_JETPACK to select version")
			} else {
				if l4t, err := strconv.Atoi(string(m[1])); err == nil {
					// Note: mapping from L4t -> JP is inconsistent (can't just subtract 30)
					// https://developer.nvidia.com/embedded/jetpack-archive
					switch l4t {
					case 35:
						return "jetpack5"
					case 36:
						return "jetpack6"
					default:
						slog.Info("unsupported L4T version", "nv_tegra_release", string(data))
					}
				}
			}
		}
	}

	// For non-ARM64 or when Jetson details are not available,
	// if the driver is older than version 12.0, use the v11 library.
	if gpuInfo.DriverMajor < 12 || (gpuInfo.DriverMajor == 12 && gpuInfo.DriverMinor == 0) {
		return "v11"
	}
	return "v12"
}
