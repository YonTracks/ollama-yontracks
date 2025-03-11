//go:build linux || windows

package discover

/*
#cgo linux LDFLAGS: -lrt -lpthread -ldl -lstdc++ -lm
#cgo windows LDFLAGS: -lpthread

#include "gpu_info.h"
*/
import "C"

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/format"
)

// ------------------------------
// Type Definitions
// ------------------------------

type cudaHandles struct {
	deviceCount int
	cudart      *C.cudart_handle_t
	nvcuda      *C.nvcuda_handle_t
	nvml        *C.nvml_handle_t
}

type oneapiHandles struct {
	oneapi      *C.oneapi_handle_t
	deviceCount int
}

// ------------------------------
// Constants and Global Variables
// ------------------------------

const (
	cudaMinimumMemory = 457 * format.MebiByte
	rocmMinimumMemory = 457 * format.MebiByte
	// TODO: OneAPI minimum memory
)

const (
	IGPUMemLimit = 1 * format.GibiByte // 1 GiB threshold for integrated GPUs
)

// With our current CUDA compile flags, GPUs older than 5.0 will not work properly.
// (string values are used to allow ldflags overrides at build time)
var (
	CudaComputeMajorMin = "5"
	CudaComputeMinorMin = "0"
)

var RocmComputeMajorMin = "9"

// Mutex and slices for discovered GPUs and system info.
var (
	gpuMutex      sync.Mutex
	bootstrapped  bool
	cpus          []CPUInfo
	cudaGPUs      []CudaGPUInfo
	nvcudaLibPath string
	cudartLibPath string
	oneapiLibPath string
	nvmlLibPath   string
	rocmGPUs      []RocmGPUInfo
	oneapiGPUs    []OneapiGPUInfo

	// For reporting incompatible GPUs.
	unsupportedGPUs []UnsupportedGPUInfo

	// For tracking bootstrap errors.
	bootstrapErrors []error
)

// ------------------------------
// Helper Functions for Environment Normalization & Validation
// ------------------------------
// uuidRegex matches a standard UUID format.
var uuidRegex = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// isUUID returns true if s matches a UUID pattern.
func isUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

// init normalizes GPU-related environment variables early.
func init() {
	keysToNormalize := []string{
		"CUDA_VISIBLE_DEVICES",
		"HIP_VISIBLE_DEVICES",
		"ROCR_VISIBLE_DEVICES",
	}
	for _, key := range keysToNormalize {
		v := envconfig.Var(key)
		// Skip if empty or CPU-only mode.
		if v == "" || v == "-1" {
			continue
		}
		// If the value is not a number and doesn't already start with "GPU-", update it.
		if _, err := strconv.Atoi(v); err != nil && !strings.HasPrefix(v, "GPU-") {
			newVal := "GPU-" + v
			slog.Info("Early normalization: updating "+key+" to expected format", "old", v, "new", newVal)
			os.Setenv(key, newVal)
		}
	}
}

// normalizeGPUEnvValue trims whitespace and removes wrapping quotes.
func normalizeGPUEnvValue(val string) string {
	trimmed := strings.TrimSpace(val)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') ||
			(trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}

// filterDiscovered filters the discovered GPU list based on the environment variable key.
func filterDiscovered(key string, discovered []GpuInfo) []GpuInfo {
	switch key {
	case "CUDA_VISIBLE_DEVICES":
		var filtered []GpuInfo
		for _, gpu := range discovered {
			if gpu.Library == "cuda" {
				filtered = append(filtered, gpu)
			}
		}
		return filtered
	case "HIP_VISIBLE_DEVICES", "ROCR_VISIBLE_DEVICES", "GPU_DEVICE_ORDINAL", "HSA_OVERRIDE_GFX_VERSION":
		var filtered []GpuInfo
		// Assume non-CUDA devices for these keys.
		for _, gpu := range discovered {
			if gpu.Library != "cuda" {
				filtered = append(filtered, gpu)
			}
		}
		return filtered
	default:
		return discovered
	}
}

// ValidateGpuEnv validates the GPU-related environment variable based on the discovered GPUs.
// It supports comma-separated tokens (numeric indices or UUIDs) and forces CPU-only mode ("-1")
// if any token is invalid.
func ValidateGpuEnv(key string, discovered []GpuInfo) string {
	// Filter discovered GPUs for the specific env var.
	filtered := filterDiscovered(key, discovered)
	slog.Debug("ValidateGpuEnv: discovered GPUs count", "key", key, "count", len(filtered))

	raw := envconfig.Var(key)
	normalized := normalizeGPUEnvValue(raw)
	if normalized == "" {
		// Not set, so use default (all GPUs).
		return ""
	}
	if normalized == "-1" {
		return "-1"
	}

	parts := strings.Split(normalized, ",")
	var validIndices []string
	for _, part := range parts {
		token := normalizeGPUEnvValue(part)

		// If token is a raw UUID (without "GPU-"), add the prefix.
		if isUUID(token) && !strings.HasPrefix(token, "GPU-") {
			token = "GPU-" + token
			slog.Debug("ValidateGpuEnv: normalized raw UUID", "key", key, "token", token)
		}

		if token == "-1" {
			// CPU-only mode.
			return "-1"
		}

		// Try to parse as numeric index.
		if index, err := strconv.Atoi(token); err == nil {
			if index < 0 || index >= len(filtered) {
				slog.Warn("Invalid "+key+" value: index out of range", "value", token, "gpuCount", len(filtered))
				return "-1"
			}
			validIndices = append(validIndices, strconv.Itoa(index))
			continue
		}

		// For non-numeric tokens, compare after trimming the "GPU-" prefix.
		trimmedToken := strings.TrimPrefix(token, "GPU-")
		found := false
		for i, gpu := range filtered {
			id := strings.TrimPrefix(gpu.ID, "GPU-")
			if id == trimmedToken {
				validIndices = append(validIndices, strconv.Itoa(i))
				found = true
				break
			}
		}
		if !found {
			slog.Warn("Invalid "+key+" value: no matching GPU UUID found", "value", token)
			return "-1"
		}
	}

	result := strings.Join(validIndices, ",")
	os.Setenv(key, result)
	return result
}

// ------------------------------
// System-Based GPU Checks & Fallback
// ------------------------------
// CheckGPUsLinux runs "lspci -nn" to detect GPUs on Linux.
func CheckGPUsLinux() ([]string, error) {
	cmd := exec.Command("lspci", "-nn")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run lspci: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	var gpus []string
	for _, line := range lines {
		if strings.Contains(line, "VGA compatible controller") || strings.Contains(line, "3D controller") {
			gpus = append(gpus, line)
		}
	}
	return gpus, nil
}

// CheckGPUsWindows runs "wmic path win32_videocontroller get name" to detect GPUs on Windows.
func CheckGPUsWindows() ([]string, error) {
	cmd := exec.Command("wmic", "path", "win32_videocontroller", "get", "name")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run wmic: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	var gpus []string
	for _, line := range lines {
		gpu := strings.TrimSpace(line)
		if gpu != "" && gpu != "Name" {
			gpus = append(gpus, gpu)
		}
	}
	return gpus, nil
}

// fallbackToCPU returns a GpuInfoList containing only CPU information.
func fallbackToCPU() GpuInfoList {
	mem, err := GetCPUMem()
	if err != nil {
		slog.Warn("fallbackToCPU: error looking up system memory", "error", err)
	}
	details, err := GetCPUDetails()
	if err != nil {
		slog.Warn("fallbackToCPU: failed to lookup CPU details", "error", err)
	}
	cpuInfo := CPUInfo{
		GpuInfo: GpuInfo{
			memInfo: mem,
			Library: "cpu",
			ID:      "0",
		},
		CPUs: details,
	}
	gpuMutex.Lock()
	cpus = []CPUInfo{cpuInfo}
	gpuMutex.Unlock()
	return GpuInfoList{cpuInfo.GpuInfo}
}

// ------------------------------
// Library Initialization Functions
// ------------------------------

func initCudaHandles() *cudaHandles {
	slog.Debug("initCudaHandles: starting CUDA library search")
	cHandles := &cudaHandles{}
	if nvmlLibPath != "" {
		slog.Debug("initCudaHandles: using cached NVML lib", "nvmlLibPath", nvmlLibPath)
		cHandles.nvml, _, _ = loadNVMLMgmt([]string{nvmlLibPath})
		return cHandles
	}
	if nvcudaLibPath != "" {
		slog.Debug("initCudaHandles: using cached NVCUDA lib", "nvcudaLibPath", nvcudaLibPath)
		cHandles.deviceCount, cHandles.nvcuda, _, _ = loadNVCUDAMgmt([]string{nvcudaLibPath})
		return cHandles
	}
	if cudartLibPath != "" {
		slog.Debug("initCudaHandles: using cached CUDART lib", "cudartLibPath", cudartLibPath)
		cHandles.deviceCount, cHandles.cudart, _, _ = loadCUDARTMgmt([]string{cudartLibPath})
		return cHandles
	}

	var cudartMgmtPatterns []string
	nvcudaMgmtPatterns := NvcudaGlobs
	cudartMgmtPatterns = append(cudartMgmtPatterns, filepath.Join(LibOllamaPath, "cuda_v*", CudartMgmtName))
	cudartMgmtPatterns = append(cudartMgmtPatterns, CudartGlobs...)
	if len(NvmlGlobs) > 0 {
		nvmlLibPaths := FindGPULibs(NvmlMgmtName, NvmlGlobs)
		slog.Debug("initCudaHandles: found NVML library paths", "paths", nvmlLibPaths)
		if len(nvmlLibPaths) > 0 {
			nvml, libPath, err := loadNVMLMgmt(nvmlLibPaths)
			if nvml != nil {
				slog.Debug("initCudaHandles: NVML loaded", "library", libPath)
				cHandles.nvml = nvml
				nvmlLibPath = libPath
			}
			if err != nil {
				bootstrapErrors = append(bootstrapErrors, err)
			}
		}
	}

	nvcudaLibPaths := FindGPULibs(NvcudaMgmtName, nvcudaMgmtPatterns)
	slog.Debug("initCudaHandles: found NVCUDA library paths", "paths", nvcudaLibPaths)
	if len(nvcudaLibPaths) > 0 {
		deviceCount, nvcuda, libPath, err := loadNVCUDAMgmt(nvcudaLibPaths)
		if nvcuda != nil {
			slog.Debug("initCudaHandles: detected GPUs", "count", deviceCount, "library", libPath)
			cHandles.nvcuda = nvcuda
			cHandles.deviceCount = deviceCount
			nvcudaLibPath = libPath
			return cHandles
		}
		if err != nil {
			bootstrapErrors = append(bootstrapErrors, err)
		}
	}

	cudartLibPaths := FindGPULibs(CudartMgmtName, cudartMgmtPatterns)
	slog.Debug("initCudaHandles: found CUDART library paths", "paths", cudartLibPaths)
	if len(cudartLibPaths) > 0 {
		deviceCount, cudart, libPath, err := loadCUDARTMgmt(cudartLibPaths)
		if cudart != nil {
			slog.Debug("initCudaHandles: detected GPUs", "library", libPath, "count", deviceCount)
			cHandles.cudart = cudart
			cHandles.deviceCount = deviceCount
			cudartLibPath = libPath
			return cHandles
		}
		if err != nil {
			bootstrapErrors = append(bootstrapErrors, err)
		}
	}

	return cHandles
}

func initOneAPIHandles() *oneapiHandles {
	slog.Debug("initOneAPIHandles: starting oneAPI library search")
	oHandles := &oneapiHandles{}
	if oneapiLibPath != "" {
		slog.Debug("initOneAPIHandles: using cached oneAPI lib", "oneapiLibPath", oneapiLibPath)
		oHandles.deviceCount, oHandles.oneapi, _, _ = loadOneapiMgmt([]string{oneapiLibPath})
		return oHandles
	}
	oneapiLibPaths := FindGPULibs(OneapiMgmtName, OneapiGlobs)
	slog.Debug("initOneAPIHandles: found oneAPI library paths", "paths", oneapiLibPaths)
	if len(oneapiLibPaths) > 0 {
		var err error
		oHandles.deviceCount, oHandles.oneapi, oneapiLibPath, err = loadOneapiMgmt(oneapiLibPaths)
		if err != nil {
			bootstrapErrors = append(bootstrapErrors, err)
		}
	}
	return oHandles
}

// ------------------------------
// GPU & CPU Discovery Functions
// ------------------------------

func GetCPUInfo() GpuInfoList {
	gpuMutex.Lock()
	if !bootstrapped {
		gpuMutex.Unlock()
		slog.Debug("GetCPUInfo: bootstrapping GPUs since not bootstrapped")
		GetGPUInfo()
	} else {
		gpuMutex.Unlock()
	}
	if len(cpus) == 0 {
		slog.Warn("GetCPUInfo: cpus slice is empty, returning empty list")
		return GpuInfoList{}
	}
	return GpuInfoList{cpus[0].GpuInfo}
}

// GetGPUInfo performs GPU discovery (with system-level checks and env var validation)
// and returns a GpuInfoList. It supports comma-separated GPU selections and will fallback
// to CPU-only mode if any GPU env variable is invalid.
func GetGPUInfo() GpuInfoList {
	// Normalize GPU environment variables for raw UUIDs.
	keysToNormalize := []string{
		"CUDA_VISIBLE_DEVICES",
		"HIP_VISIBLE_DEVICES",
		"ROCR_VISIBLE_DEVICES",
	}
	for _, key := range keysToNormalize {
		v := envconfig.Var(key)
		if v == "" || v == "-1" {
			continue
		}
		if _, err := strconv.Atoi(v); err != nil && !strings.HasPrefix(v, "GPU-") {
			newVal := "GPU-" + v
			slog.Info("Updating "+key+" to expected format", "old", v, "new", newVal)
			os.Setenv(key, newVal)
		}
	}

	var detectedGPUs []string
	var err error
	if runtime.GOOS == "linux" {
		detectedGPUs, err = CheckGPUsLinux()
	} else if runtime.GOOS == "windows" {
		detectedGPUs, err = CheckGPUsWindows()
	}
	if err != nil {
		slog.Warn("GPU detection via system tools failed", "error", err)
	} else if len(detectedGPUs) > 0 {
		slog.Info("System detected GPUs:", "gpus", detectedGPUs)
	}

	gpuEnvVars := []string{
		"CUDA_VISIBLE_DEVICES",
		"HIP_VISIBLE_DEVICES",
		"ROCR_VISIBLE_DEVICES",
		"GPU_DEVICE_ORDINAL",
		"HSA_OVERRIDE_GFX_VERSION",
	}
	for _, key := range gpuEnvVars {
		v := envconfig.Var(key)
		if v == "-1" {
			slog.Info("Skipping GPU discovery: " + key + " is invalid (or '-1'), using CPU-only mode")
			return fallbackToCPU()
		}
	}

	gpuMutex.Lock()
	defer gpuMutex.Unlock()
	needRefresh := true
	var cHandles *cudaHandles
	var oHandles *oneapiHandles
	defer func() {
		if cHandles != nil {
			if cHandles.cudart != nil {
				C.cudart_release(*cHandles.cudart)
			}
			if cHandles.nvcuda != nil {
				C.nvcuda_release(*cHandles.nvcuda)
			}
			if cHandles.nvml != nil {
				C.nvml_release(*cHandles.nvml)
			}
		}
		if oHandles != nil {
			if oHandles.oneapi != nil {
				// TODO: is this needed?
				C.oneapi_release(*oHandles.oneapi)
			}
		}
	}()

	if !bootstrapped {
		slog.Info("GetGPUInfo: looking for compatible GPUs")
		cudaComputeMajorMin, err := strconv.Atoi(CudaComputeMajorMin)
		if err != nil {
			slog.Error("GetGPUInfo: invalid CudaComputeMajorMin setting", "value", CudaComputeMajorMin, "error", err)
		}
		cudaComputeMinorMin, err := strconv.Atoi(CudaComputeMinorMin)
		if err != nil {
			slog.Error("GetGPUInfo: invalid CudaComputeMinorMin setting", "value", CudaComputeMinorMin, "error", err)
		}
		bootstrapErrors = []error{}
		needRefresh = false
		var memInfo C.mem_info_t

		mem, err := GetCPUMem()
		if err != nil {
			slog.Warn("GetGPUInfo: error looking up system memory", "error", err)
		}
		details, err := GetCPUDetails()
		if err != nil {
			slog.Warn("GetGPUInfo: failed to lookup CPU details", "error", err)
		}
		cpus = []CPUInfo{
			{
				GpuInfo: GpuInfo{
					memInfo: mem,
					Library: "cpu",
					ID:      "0",
				},
				CPUs: details,
			},
		}

		cHandles = initCudaHandles()
		// NVIDIA GPU discovery.
		for i := range cHandles.deviceCount {
			if cHandles.cudart != nil || cHandles.nvcuda != nil {
				gpuInfo := CudaGPUInfo{
					GpuInfo: GpuInfo{
						Library: "cuda",
					},
					index: i,
				}
				var driverMajor int
				var driverMinor int
				if cHandles.cudart != nil {
					C.cudart_bootstrap(*cHandles.cudart, C.int(i), &memInfo)
				} else {
					C.nvcuda_bootstrap(*cHandles.nvcuda, C.int(i), &memInfo)
					driverMajor = int(cHandles.nvcuda.driver_major)
					driverMinor = int(cHandles.nvcuda.driver_minor)
				}
				if memInfo.err != nil {
					slog.Info("GetGPUInfo: error looking up NVIDIA GPU memory", "error", C.GoString(memInfo.err))
					C.free(unsafe.Pointer(memInfo.err))
					continue
				}
				gpuInfo.TotalMemory = uint64(memInfo.total)
				gpuInfo.FreeMemory = uint64(memInfo.free)
				gpuInfo.ID = C.GoString(&memInfo.gpu_id[0])
				gpuInfo.Compute = fmt.Sprintf("%d.%d", memInfo.major, memInfo.minor)
				gpuInfo.computeMajor = int(memInfo.major)
				gpuInfo.computeMinor = int(memInfo.minor)
				gpuInfo.MinimumMemory = cudaMinimumMemory
				gpuInfo.DriverMajor = driverMajor
				gpuInfo.DriverMinor = driverMinor
				variant := cudaVariant(gpuInfo)
				// Use bundled libraries first.
				if variant != "" {
					variantPath := filepath.Join(LibOllamaPath, "cuda_"+variant)
					if _, err := os.Stat(variantPath); err == nil {
						slog.Debug("GetGPUInfo: using variant directory", "variantPath", variantPath)
						gpuInfo.DependencyPath = append([]string{variantPath}, gpuInfo.DependencyPath...)
					}
				}
				gpuInfo.Name = C.GoString(&memInfo.gpu_name[0])
				gpuInfo.Variant = variant
				if int(memInfo.major) < cudaComputeMajorMin || (int(memInfo.major) == cudaComputeMajorMin && int(memInfo.minor) < cudaComputeMinorMin) {
					slog.Info("GetGPUInfo: CUDA GPU too old", "index", i, "compute", fmt.Sprintf("%d.%d", memInfo.major, memInfo.minor))
					unsupportedGPUs = append(unsupportedGPUs, UnsupportedGPUInfo{GpuInfo: gpuInfo.GpuInfo})
					continue
				}
				// Query the management library for OS VRAM overhead.
				if cHandles.nvml != nil {
					uuid := C.CString(gpuInfo.ID)
					defer C.free(unsafe.Pointer(uuid))
					C.nvml_get_free(*cHandles.nvml, uuid, &memInfo.free, &memInfo.total, &memInfo.used)
					if memInfo.err != nil {
						slog.Warn("GetGPUInfo: error looking up NVIDIA GPU memory (NVML)", "error", C.GoString(memInfo.err))
						C.free(unsafe.Pointer(memInfo.err))
					} else {
						if memInfo.free != 0 && uint64(memInfo.free) > gpuInfo.FreeMemory {
							gpuInfo.OSOverhead = uint64(memInfo.free) - gpuInfo.FreeMemory
							slog.Info("GetGPUInfo: detected OS VRAM overhead",
								"id", gpuInfo.ID,
								"library", gpuInfo.Library,
								"compute", gpuInfo.Compute,
								"driver", fmt.Sprintf("%d.%d", gpuInfo.DriverMajor, gpuInfo.DriverMinor),
								"name", gpuInfo.Name,
								"overhead", format.HumanBytes2(gpuInfo.OSOverhead),
							)
						}
					}
				}
				slog.Debug("GetGPUInfo: adding discovered CUDA GPU", "id", gpuInfo.ID)
				cudaGPUs = append(cudaGPUs, gpuInfo)
			}
		}
		// Intel oneAPI GPU discovery.
		if envconfig.IntelGPU() {
			slog.Debug("GetGPUInfo: attempting Intel GPU discovery")
			oHandles = initOneAPIHandles()
			if oHandles != nil && oHandles.oneapi != nil {
				for d := range oHandles.oneapi.num_drivers {
					if oHandles.oneapi == nil {
						slog.Warn("GetGPUInfo: nil oneAPI handle despite driver count", "count", int(oHandles.oneapi.num_drivers))
						continue
					}
					devCount := C.oneapi_get_device_count(*oHandles.oneapi, C.int(d))
					for i := range devCount {
						gpuInfo := OneapiGPUInfo{
							GpuInfo: GpuInfo{
								Library: "oneapi",
							},
							driverIndex: int(d),
							gpuIndex:    int(i),
						}
						C.oneapi_check_vram(*oHandles.oneapi, C.int(d), C.int(i), &memInfo)
						// Reserve 5% of VRAM.
						totalFreeMem := float64(memInfo.free) * 0.95
						memInfo.free = C.uint64_t(totalFreeMem)
						gpuInfo.TotalMemory = uint64(memInfo.total)
						gpuInfo.FreeMemory = uint64(memInfo.free)
						gpuInfo.ID = C.GoString(&memInfo.gpu_id[0])
						gpuInfo.Name = C.GoString(&memInfo.gpu_name[0])
						gpuInfo.DependencyPath = []string{LibOllamaPath}
						slog.Debug("GetGPUInfo: adding discovered oneAPI GPU", "id", gpuInfo.ID)
						oneapiGPUs = append(oneapiGPUs, gpuInfo)
					}
				}
			}
		}
		// ROCm GPU discovery.
		rocmGPUs, err = AMDGetGPUInfo()
		if err != nil {
			slog.Warn("GetGPUInfo: error in ROCm GPU discovery", "error", err)
			bootstrapErrors = append(bootstrapErrors, err)
		}
		bootstrapped = true

		if len(cudaGPUs) == 0 && len(rocmGPUs) == 0 && len(oneapiGPUs) == 0 {
			slog.Info("GetGPUInfo: no compatible GPUs were discovered")
		}
		// TODO: verify runners for discovered GPUs and filter unsupported ones.
	}

	if len(cudaGPUs) == 0 && len(rocmGPUs) == 0 && len(oneapiGPUs) == 0 && len(detectedGPUs) > 0 {
		slog.Warn("GPUs detected by system tools, but not by libraries. Possible driver/library issue?", "gpus", detectedGPUs)
	}
	// Refresh free memory usage if needed.
	if needRefresh {
		mem, err := GetCPUMem()
		if err != nil {
			slog.Warn("GetGPUInfo: error looking up system memory during refresh", "error", err)
		} else {
			slog.Debug("GetGPUInfo: updating system memory data",
				slog.Group("before", "total", format.HumanBytes2(cpus[0].TotalMemory), "free", format.HumanBytes2(cpus[0].FreeMemory), "free_swap", format.HumanBytes2(cpus[0].FreeSwap)),
				slog.Group("now", "total", format.HumanBytes2(mem.TotalMemory), "free", format.HumanBytes2(mem.FreeMemory), "free_swap", format.HumanBytes2(mem.FreeSwap)),
			)
			cpus[0].FreeMemory = mem.FreeMemory
			cpus[0].FreeSwap = mem.FreeSwap
		}

		var memInfo C.mem_info_t
		if cHandles == nil && len(cudaGPUs) > 0 {
			cHandles = initCudaHandles()
		}
		for i, gpu := range cudaGPUs {
			if cHandles.nvml != nil {
				uuid := C.CString(gpu.ID)
				defer C.free(unsafe.Pointer(uuid))
				C.nvml_get_free(*cHandles.nvml, uuid, &memInfo.free, &memInfo.total, &memInfo.used)
			} else if cHandles.cudart != nil {
				C.cudart_bootstrap(*cHandles.cudart, C.int(gpu.index), &memInfo)
			} else if cHandles.nvcuda != nil {
				C.nvcuda_get_free(*cHandles.nvcuda, C.int(gpu.index), &memInfo.free, &memInfo.total)
				memInfo.used = memInfo.total - memInfo.free
			} else {
				slog.Warn("GetGPUInfo: no valid CUDA library loaded to refresh VRAM usage")
				break
			}
			if memInfo.err != nil {
				slog.Warn("GetGPUInfo: error refreshing GPU memory", "error", C.GoString(memInfo.err))
				C.free(unsafe.Pointer(memInfo.err))
				continue
			}
			if memInfo.free == 0 {
				slog.Warn("GetGPUInfo: GPU memory refresh returned 0 free memory")
				continue
			}
			if cHandles.nvml != nil && gpu.OSOverhead > 0 {
				memInfo.free -= C.uint64_t(gpu.OSOverhead)
			}
			slog.Debug("GetGPUInfo: updating CUDA memory data",
				"gpu", gpu.ID,
				"name", gpu.Name,
				"overhead", format.HumanBytes2(gpu.OSOverhead),
				slog.Group("before", "total", format.HumanBytes2(gpu.TotalMemory), "free", format.HumanBytes2(gpu.FreeMemory)),
				slog.Group("now", "total", format.HumanBytes2(uint64(memInfo.total)), "free", format.HumanBytes2(uint64(memInfo.free)), "used", format.HumanBytes2(uint64(memInfo.used))),
			)
			cudaGPUs[i].FreeMemory = uint64(memInfo.free)
		}

		if oHandles == nil && len(oneapiGPUs) > 0 {
			oHandles = initOneAPIHandles()
		}
		for i, gpu := range oneapiGPUs {
			if oHandles.oneapi == nil {
				slog.Warn("GetGPUInfo: nil oneAPI handle during memory refresh", "deviceCount", oHandles.deviceCount)
				continue
			}
			C.oneapi_check_vram(*oHandles.oneapi, C.int(gpu.driverIndex), C.int(gpu.gpuIndex), &memInfo)
			totalFreeMem := float64(memInfo.free) * 0.95
			memInfo.free = C.uint64_t(totalFreeMem)
			oneapiGPUs[i].FreeMemory = uint64(memInfo.free)
		}

		err = RocmGPUInfoList(rocmGPUs).RefreshFreeMemory()
		if err != nil {
			slog.Debug("GetGPUInfo: problem refreshing ROCm free memory", "error", err)
		}
	}
	// Compile the final list of GPUs.
	resp := []GpuInfo{}
	for _, gpu := range cudaGPUs {
		resp = append(resp, gpu.GpuInfo)
	}
	for _, gpu := range rocmGPUs {
		resp = append(resp, gpu.GpuInfo)
	}
	for _, gpu := range oneapiGPUs {
		resp = append(resp, gpu.GpuInfo)
	}
	// Validate each GPU-related environment variable.
	if len(resp) == 0 {
		slog.Warn("GetGPUInfo: no GPUs detected by libraries, falling back to CPU-only mode")
		return fallbackToCPU()
	}

	validCuda := ValidateGpuEnv("CUDA_VISIBLE_DEVICES", resp)
	if validCuda == "-1" {
		slog.Warn("CUDA_VISIBLE_DEVICES env value is invalid, falling back to CPU-only mode")
		return fallbackToCPU()
	}
	validHip := ValidateGpuEnv("HIP_VISIBLE_DEVICES", resp)
	if validHip == "-1" {
		slog.Warn("HIP_VISIBLE_DEVICES env value is invalid, falling back to CPU-only mode")
		return fallbackToCPU()
	}
	validRocr := ValidateGpuEnv("ROCR_VISIBLE_DEVICES", resp)
	if validRocr == "-1" {
		slog.Warn("ROCR_VISIBLE_DEVICES env value is invalid, falling back to CPU-only mode")
		return fallbackToCPU()
	}
	validOrdinal := ValidateGpuEnv("GPU_DEVICE_ORDINAL", resp)
	if validOrdinal == "-1" {
		slog.Warn("GPU_DEVICE_ORDINAL env value is invalid, falling back to CPU-only mode")
		return fallbackToCPU()
	}

	slog.Debug("GetGPUInfo: final discovered GPU info", "count", len(resp))
	return resp
}

// GetVisibleDevicesEnv returns the environment variable name and value to select GPUs
// based on the discovered GPU info.
func (l GpuInfoList) GetVisibleDevicesEnv() (string, string) {
	if len(l) == 0 {
		slog.Debug("GetVisibleDevicesEnv: empty GPU list")
		return "", ""
	}
	switch l[0].Library {
	case "cuda":
		return cudaGetVisibleDevicesEnv(l)
	case "rocm":
		return rocmGetVisibleDevicesEnv(l)
	case "oneapi":
		return oneapiGetVisibleDevicesEnv(l)
	default:
		slog.Debug("GetVisibleDevicesEnv: no filter required for library " + l[0].Library)
		return "", ""
	}
}

// GetSystemInfo collects system memory and GPU discovery information.
func GetSystemInfo() SystemInfo {
	gpus := GetGPUInfo()
	gpuMutex.Lock()
	defer gpuMutex.Unlock()
	discoveryErrors := []string{}
	for _, err := range bootstrapErrors {
		discoveryErrors = append(discoveryErrors, err.Error())
	}
	if len(gpus) == 1 && gpus[0].Library == "cpu" {
		slog.Debug("GetSystemInfo: only CPU info present, returning empty GPU list")
		gpus = []GpuInfo{}
	}
	sysInfo := SystemInfo{
		System:          cpus[0],
		GPUs:            gpus,
		UnsupportedGPUs: unsupportedGPUs,
		DiscoveryErrors: discoveryErrors,
	}
	slog.Debug("GetSystemInfo: final system info", "system", sysInfo.System, "GPU count", len(sysInfo.GPUs))
	return sysInfo
}

// ------------------------------
// Library Bootstrap Functions
// ------------------------------
// FindGPULibs searches for GPU libraries using bundled paths, system library paths, and default patterns.
func FindGPULibs(baseLibName string, defaultPatterns []string) []string {
	gpuLibPaths := []string{}
	slog.Debug("FindGPULibs: searching for GPU library", "name", baseLibName)
	// Search bundled libraries first.
	patterns := []string{filepath.Join(LibOllamaPath, baseLibName)}

	var ldPaths []string
	switch runtime.GOOS {
	case "windows":
		ldPaths = strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	case "linux":
		ldPaths = strings.Split(os.Getenv("LD_LIBRARY_PATH"), string(os.PathListSeparator))
	}
	// Search system library paths.
	for _, p := range ldPaths {
		p, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		patterns = append(patterns, filepath.Join(p, baseLibName))
	}
	// Append default patterns.
	patterns = append(patterns, defaultPatterns...)
	slog.Debug("FindGPULibs: using glob patterns", "globs", patterns)
	for _, pattern := range patterns {
		if strings.Contains(pattern, "PhysX") {
			slog.Debug("FindGPULibs: skipping PhysX cuda library path", "path", pattern)
			continue
		}
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			// Resolve links to avoid duplicates.
			libPath := match
			tmp := match
			var err error
			for ; err == nil; tmp, err = os.Readlink(libPath) {
				if !filepath.IsAbs(tmp) {
					tmp = filepath.Join(filepath.Dir(libPath), tmp)
				}
				libPath = tmp
			}
			if !slices.Contains(gpuLibPaths, libPath) {
				slog.Debug("FindGPULibs: adding discovered library", "libPath", libPath)
				gpuLibPaths = append(gpuLibPaths, libPath)
			}
		}
	}
	slog.Debug("FindGPULibs: discovered GPU libraries", "paths", gpuLibPaths)
	return gpuLibPaths
}

// Bootstrap the CUDART management library.
// Returns: num devices, handle, libPath, error.
func loadCUDARTMgmt(cudartLibPaths []string) (int, *C.cudart_handle_t, string, error) {
	var resp C.cudart_init_resp_t
	resp.ch.verbose = getVerboseState()
	var err error
	for _, libPath := range cudartLibPaths {
		lib := C.CString(libPath)
		defer C.free(unsafe.Pointer(lib))
		C.cudart_init(lib, &resp)
		if resp.err != nil {
			err = fmt.Errorf("unable to load cudart library %s: %s", libPath, C.GoString(resp.err))
			slog.Debug("loadCUDARTMgmt: error", "err", err.Error())
			C.free(unsafe.Pointer(resp.err))
		} else {
			slog.Debug("loadCUDARTMgmt: successfully loaded", "libPath", libPath, "deviceCount", resp.num_devices)
			return int(resp.num_devices), &resp.ch, libPath, nil
		}
	}
	return 0, nil, "", err
}

// Bootstrap the NVCUDA management library.
// Returns: num devices, handle, libPath, error.
func loadNVCUDAMgmt(nvcudaLibPaths []string) (int, *C.nvcuda_handle_t, string, error) {
	var resp C.nvcuda_init_resp_t
	resp.ch.verbose = getVerboseState()
	var err error
	for _, libPath := range nvcudaLibPaths {
		lib := C.CString(libPath)
		defer C.free(unsafe.Pointer(lib))
		C.nvcuda_init(lib, &resp)
		if resp.err != nil {
			switch resp.cudaErr {
			case C.CUDA_ERROR_INSUFFICIENT_DRIVER, C.CUDA_ERROR_SYSTEM_DRIVER_MISMATCH:
				err = fmt.Errorf("version mismatch between driver and cuda driver library - reboot or upgrade may be required: library %s", libPath)
				slog.Warn("loadNVCUDAMgmt:", "error", err)
			case C.CUDA_ERROR_NO_DEVICE:
				err = fmt.Errorf("no nvidia devices detected by library %s", libPath)
				slog.Info("loadNVCUDAMgmt:", "error", err)
			case C.CUDA_ERROR_UNKNOWN:
				err = fmt.Errorf("unknown error initializing cuda driver library %s: %s. see https://github.com/ollama/ollama/blob/main/docs/troubleshooting.md for more information", libPath, C.GoString(resp.err))
				slog.Warn("loadNVCUDAMgmt:", "error", err)
			default:
				msg := C.GoString(resp.err)
				if strings.Contains(msg, "wrong ELF class") {
					slog.Debug("loadNVCUDAMgmt: skipping 32bit library", "library", libPath)
				} else {
					err = fmt.Errorf("unable to load cudart library %s: %s", libPath, C.GoString(resp.err))
					slog.Info("loadNVCUDAMgmt:", "error", err)
				}
			}
			C.free(unsafe.Pointer(resp.err))
		} else {
			slog.Debug("loadNVCUDAMgmt: successfully loaded", "libPath", libPath, "deviceCount", resp.num_devices)
			return int(resp.num_devices), &resp.ch, libPath, nil
		}
	}
	return 0, nil, "", err
}

// Bootstrap the NVML management library.
// Returns: handle, libPath, error.
func loadNVMLMgmt(nvmlLibPaths []string) (*C.nvml_handle_t, string, error) {
	var resp C.nvml_init_resp_t
	resp.ch.verbose = getVerboseState()
	var err error
	for _, libPath := range nvmlLibPaths {
		lib := C.CString(libPath)
		defer C.free(unsafe.Pointer(lib))
		C.nvml_init(lib, &resp)
		if resp.err != nil {
			err = fmt.Errorf("unable to load NVML management library %s: %s", libPath, C.GoString(resp.err))
			slog.Info("loadNVMLMgmt:", "error", err)
			C.free(unsafe.Pointer(resp.err))
		} else {
			slog.Debug("loadNVMLMgmt: successfully loaded", "libPath", libPath)
			return &resp.ch, libPath, nil
		}
	}
	return nil, "", err
}

// Bootstrap the oneAPI management library.
// Returns: num devices, handle, libPath, error.
func loadOneapiMgmt(oneapiLibPaths []string) (int, *C.oneapi_handle_t, string, error) {
	var resp C.oneapi_init_resp_t
	num_devices := 0
	resp.oh.verbose = getVerboseState()
	var err error
	for _, libPath := range oneapiLibPaths {
		lib := C.CString(libPath)
		defer C.free(unsafe.Pointer(lib))
		C.oneapi_init(lib, &resp)
		if resp.err != nil {
			err = fmt.Errorf("unable to load oneAPI management library %s: %s", libPath, C.GoString(resp.err))
			slog.Debug("loadOneapiMgmt:", "error", err)
			C.free(unsafe.Pointer(resp.err))
		} else {
			for i := range resp.oh.num_drivers {
				num_devices += int(C.oneapi_get_device_count(resp.oh, C.int(i)))
			}
			slog.Debug("loadOneapiMgmt: successfully loaded", "libPath", libPath, "deviceCount", num_devices)
			return num_devices, &resp.oh, libPath, nil
		}
	}
	return 0, nil, "", err
}

func getVerboseState() C.uint16_t {
	if envconfig.Debug() {
		slog.Debug("getVerboseState: Debug mode enabled")
		return C.uint16_t(1)
	}
	return C.uint16_t(0)
}
