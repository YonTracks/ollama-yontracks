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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/format"
)

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

const (
	cudaMinimumMemory = 457 * format.MebiByte
	rocmMinimumMemory = 457 * format.MebiByte
	// TODO OneAPI minimum memory
)

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

	// If any discovered GPUs are incompatible, report why
	unsupportedGPUs []UnsupportedGPUInfo

	// Keep track of errors during bootstrapping so that if GPUs are missing
	// they expected to be present this may explain why
	bootstrapErrors []error
)

// With our current CUDA compile flags, older than 5.0 will not work properly
var (
	CudaComputeMajorMin = "5"
	CudaComputeMinorMin = "0"
)

var RocmComputeMajorMin = "9"

// TODO: find a better way to detect iGPU instead of minimum memory.
const IGPUMemLimit = 1 * format.GibiByte // 512G is what they typically report, so anything less than 1G must be iGPU

// initCudaHandles initializes CUDA handles.
// Note: gpuMutex must already be held.
func initCudaHandles() *cudaHandles {
	slog.Debug("initCudaHandles: starting CUDA library search")
	cHandles := &cudaHandles{}
	// Short-circuit if we already know which library to use.
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

// initOneAPIHandles bootstraps the oneAPI library.
// Note: gpuMutex must already be held.
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

// GetCPUInfo returns CPU info wrapped as a GpuInfoList.
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

// GetGPUInfo performs GPU discovery. If any GPU-related env variable is empty or set to "-1", CPU-only mode is used.
func GetGPUInfo() GpuInfoList {
	// Check if any GPU-related env variable indicates CPU-only mode.
	gpuEnvVars := []string{
		"CUDA_VISIBLE_DEVICES",
		"HIP_VISIBLE_DEVICES",
		"ROCR_VISIBLE_DEVICES",
		"GPU_DEVICE_ORDINAL",
		"HSA_OVERRIDE_GFX_VERSION",
	}
	for _, key := range gpuEnvVars {
		v := envconfig.Var(key)
		slog.Debug("GetGPUInfo: checking env variable", "key", key, "value", v)
		if v == "-1" {
			slog.Info("Skipping GPU discovery: " + key + " is invalid (or '-1'), using CPU-only mode")
			mem, err := GetCPUMem()
			if err != nil {
				slog.Warn("GetGPUInfo: error looking up system memory", "error", err)
			}
			details, err := GetCPUDetails()
			if err != nil {
				slog.Warn("GetGPUInfo: failed to lookup CPU details", "error", err)
			}
			cpuInfo := CPUInfo{
				GpuInfo: GpuInfo{
					memInfo: mem,
					Library: "cpu",
					ID:      "0",
				},
				CPUs: details,
			}
			slog.Info("GetGPUInfo: CPU-only mode activated due to " + key)
			// Populate global cpus slice.
			gpuMutex.Lock()
			cpus = []CPUInfo{cpuInfo}
			gpuMutex.Unlock()
			return GpuInfoList{cpuInfo.GpuInfo}
		}
	}

	slog.Debug("GetGPUInfo:", "CUDA_VISIBLE_DEVICES (envconfig)", envconfig.CudaVisibleDevices())
	slog.Debug("GetGPUInfo:", "HIP_VISIBLE_DEVICES (envconfig)", envconfig.HipVisibleDevices())
	// Proceed with GPU discovery if no GPU env variable forced CPU-only mode.
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

		// Load ALL libraries.
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
					slog.Info("GetGPUInfo: error looking up nvidia GPU memory", "error", C.GoString(memInfo.err))
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

				// Use bundled libraries if available.
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
					unsupportedGPUs = append(unsupportedGPUs,
						UnsupportedGPUInfo{
							GpuInfo: gpuInfo.GpuInfo,
						})
					continue
				}

				// Query the management library for additional overhead.
				if cHandles.nvml != nil {
					uuid := C.CString(gpuInfo.ID)
					defer C.free(unsafe.Pointer(uuid))
					C.nvml_get_free(*cHandles.nvml, uuid, &memInfo.free, &memInfo.total, &memInfo.used)
					if memInfo.err != nil {
						slog.Warn("GetGPUInfo: error looking up nvidia GPU memory (NVML)", "error", C.GoString(memInfo.err))
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

				// Append the discovered CUDA GPU.
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
						C.oneapi_check_vram(*oHandles.oneapi, C.int(d), i, &memInfo)
						// Reserve a fraction for MKL as a workaround.
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
		// (Additional runner verification can be added here)
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
	if len(resp) == 0 {
		slog.Debug("GetGPUInfo: no GPUs discovered, falling back to CPU info")
		resp = append(resp, cpus[0].GpuInfo)
	}
	slog.Debug("GetGPUInfo: final discovered GPU info", "count", len(resp))
	return resp
}

func FindGPULibs(baseLibName string, defaultPatterns []string) []string {
	// Multiple GPU libraries may exist; try each pattern.
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

	// Search system LD_LIBRARY_PATH.
	for _, p := range ldPaths {
		p, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		patterns = append(patterns, filepath.Join(p, baseLibName))
	}

	// Finally, use default patterns.
	patterns = append(patterns, defaultPatterns...)
	slog.Debug("FindGPULibs: using glob patterns", "globs", patterns)
	for _, pattern := range patterns {
		if strings.Contains(pattern, "PhysX") {
			slog.Debug("FindGPULibs: skipping PhysX cuda library path", "path", pattern)
			continue
		}
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			libPath := match
			tmp := match
			var err error
			for ; err == nil; tmp, err = os.Readlink(libPath) {
				if !filepath.IsAbs(tmp) {
					tmp = filepath.Join(filepath.Dir(libPath), tmp)
				}
				libPath = tmp
			}
			new := true
			for _, cmp := range gpuLibPaths {
				if cmp == libPath {
					new = false
					break
				}
			}
			if new {
				slog.Debug("FindGPULibs: adding discovered library", "libPath", libPath)
				gpuLibPaths = append(gpuLibPaths, libPath)
			}
		}
	}
	slog.Debug("FindGPULibs: discovered GPU libraries", "paths", gpuLibPaths)
	return gpuLibPaths
}

// loadCUDARTMgmt bootstraps the cudart library.
// Returns: number of devices, handle, libPath, error.
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

// loadNVCUDAMgmt bootstraps the nvcuda library.
// Returns: number of devices, handle, libPath, error.
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
			err = nil
			return int(resp.num_devices), &resp.ch, libPath, err
		}
	}
	return 0, nil, "", err
}

// loadNVMLMgmt bootstraps the NVML management library.
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
			err = nil
			return &resp.ch, libPath, err
		}
	}
	return nil, "", err
}

// loadOneapiMgmt bootstraps the oneAPI library.
// Returns: number of devices, handle, libPath, error.
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
			err = nil
			for i := range resp.oh.num_drivers {
				num_devices += int(C.oneapi_get_device_count(resp.oh, C.int(i)))
			}
			slog.Debug("loadOneapiMgmt: successfully loaded", "libPath", libPath, "deviceCount", num_devices)
			return num_devices, &resp.oh, libPath, err
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

// GetVisibleDevicesEnv returns the environment variable name and value to select GPUs based on the discovered GPU info.
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

func GetSystemInfo() SystemInfo {
	gpus := GetGPUInfo()
	gpuMutex.Lock()
	defer gpuMutex.Unlock()
	discoveryErrors := []string{}
	for _, err := range bootstrapErrors {
		discoveryErrors = append(discoveryErrors, err.Error())
	}
	// If only CPU info is present, return an empty GPU slice.
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
