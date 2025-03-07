package envconfig

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Host returns the scheme and host. Host can be configured via the OLLAMA_HOST environment variable.
// Default is scheme "http" and host "127.0.0.1:11434"
func Host() *url.URL {
	defaultPort := "11434"

	s := strings.TrimSpace(Var("OLLAMA_HOST"))
	slog.Debug("Host: raw OLLAMA_HOST", "value", s)
	scheme, hostport, ok := strings.Cut(s, "://")
	switch {
	case !ok:
		slog.Debug("Host: no scheme found, defaulting to http")
		scheme, hostport = "http", s
	case scheme == "http":
		defaultPort = "80"
		slog.Debug("Host: scheme set to http, using default port", "port", defaultPort)
	case scheme == "https":
		defaultPort = "443"
		slog.Debug("Host: scheme set to https, using default port", "port", defaultPort)
	}

	hostport, path, _ := strings.Cut(hostport, "/")
	slog.Debug("Host: after cutting path", "hostport", hostport, "path", path)
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		slog.Debug("Host: error splitting host and port, using defaults", "error", err.Error(), "hostport", hostport)
		host, port = "127.0.0.1", defaultPort
		if ip := net.ParseIP(strings.Trim(hostport, "[]")); ip != nil {
			host = ip.String()
		} else if hostport != "" {
			host = hostport
		}
	}

	if n, err := strconv.ParseInt(port, 10, 32); err != nil || n > 65535 || n < 0 {
		slog.Warn("Host: invalid port, using default", "port", port, "default", defaultPort)
		port = defaultPort
	}

	slog.Debug("Host: final computed values", "scheme", scheme, "host", host, "port", port, "path", path)
	return &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(host, port),
		Path:   path,
	}
}

// AllowedOrigins returns a list of allowed origins. AllowedOrigins can be configured via the OLLAMA_ORIGINS environment variable.
func AllowedOrigins() (origins []string) {
	if s := Var("OLLAMA_ORIGINS"); s != "" {
		origins = strings.Split(s, ",")
		slog.Debug("AllowedOrigins: parsed OLLAMA_ORIGINS", "origins", origins)
	}

	for _, origin := range []string{"localhost", "127.0.0.1", "0.0.0.0"} {
		origins = append(origins,
			fmt.Sprintf("http://%s", origin),
			fmt.Sprintf("https://%s", origin),
			fmt.Sprintf("http://%s", net.JoinHostPort(origin, "*")),
			fmt.Sprintf("https://%s", net.JoinHostPort(origin, "*")),
		)
	}

	origins = append(origins,
		"app://*",
		"file://*",
		"tauri://*",
		"vscode-webview://*",
		"vscode-file://*",
	)
	slog.Debug("AllowedOrigins: final origins list", "origins", origins)
	return origins
}

// Models returns the path to the models directory. Models directory can be configured via the OLLAMA_MODELS environment variable.
// Default is $HOME/.ollama/models
func Models() string {
	if s := Var("OLLAMA_MODELS"); s != "" {
		// slog.Debug("Models: using OLLAMA_MODELS from env", "path", s)
		return s
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Models: unable to determine user home directory", "error", err.Error())
		panic(err)
	}

	modelPath := filepath.Join(home, ".ollama", "models")
	// slog.Debug("Models: default model path", "path", modelPath)
	return modelPath
}

// KeepAlive returns the duration that models stay loaded in memory. KeepAlive can be configured via the OLLAMA_KEEP_ALIVE environment variable.
// Negative values are treated as infinite. Zero is treated as no keep alive.
// Default is 5 minutes.
func KeepAlive() (keepAlive time.Duration) {
	keepAlive = 5 * time.Minute
	if s := Var("OLLAMA_KEEP_ALIVE"); s != "" {
		slog.Debug("KeepAlive: raw value", "value", s)
		if d, err := time.ParseDuration(s); err == nil {
			keepAlive = d
			slog.Debug("KeepAlive: parsed as duration", "duration", keepAlive)
		} else if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			keepAlive = time.Duration(n) * time.Second
			slog.Debug("KeepAlive: parsed as integer seconds", "duration", keepAlive)
		} else {
			slog.Warn("KeepAlive: unable to parse, using default", "value", s, "error", err.Error())
		}
	}

	if keepAlive < 0 {
		slog.Debug("KeepAlive: negative value, treating as infinite")
		return time.Duration(math.MaxInt64)
	}

	return keepAlive
}

// LoadTimeout returns the duration for stall detection during model loads. LoadTimeout can be configured via the OLLAMA_LOAD_TIMEOUT environment variable.
// Zero or Negative values are treated as infinite.
// Default is 5 minutes.
func LoadTimeout() (loadTimeout time.Duration) {
	loadTimeout = 5 * time.Minute
	if s := Var("OLLAMA_LOAD_TIMEOUT"); s != "" {
		slog.Debug("LoadTimeout: raw value", "value", s)
		if d, err := time.ParseDuration(s); err == nil {
			loadTimeout = d
			slog.Debug("LoadTimeout: parsed as duration", "duration", loadTimeout)
		} else if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			loadTimeout = time.Duration(n) * time.Second
			slog.Debug("LoadTimeout: parsed as integer seconds", "duration", loadTimeout)
		} else {
			slog.Warn("LoadTimeout: unable to parse, using default", "value", s, "error", err.Error())
		}
	}

	if loadTimeout <= 0 {
		slog.Debug("LoadTimeout: non-positive value, treating as infinite")
		return time.Duration(math.MaxInt64)
	}

	return loadTimeout
}

func Bool(k string) func() bool {
	return func() bool {
		if s := Var(k); s != "" {
			slog.Debug("Bool: reading", "key", k, "raw", s)
			b, err := strconv.ParseBool(s)
			if err != nil {
				slog.Warn("Bool: error parsing boolean, defaulting to true", "key", k, "value", s, "error", err.Error())
				return true
			}
			slog.Debug("Bool: parsed value", "key", k, "value", b)
			return b
		}
		slog.Debug("Bool: key not set, defaulting to false", "key", k)
		return false
	}
}

var (
	// Debug enables additional debug information.
	Debug = Bool("OLLAMA_DEBUG")
	// FlashAttention enables the experimental flash attention feature.
	FlashAttention = Bool("OLLAMA_FLASH_ATTENTION")
	// KvCacheType is the quantization type for the K/V cache.
	KvCacheType = String("OLLAMA_KV_CACHE_TYPE")
	// NoHistory disables readline history.
	NoHistory = Bool("OLLAMA_NOHISTORY")
	// NoPrune disables pruning of model blobs on startup.
	NoPrune = Bool("OLLAMA_NOPRUNE")
	// SchedSpread allows scheduling models across all GPUs.
	SchedSpread = Bool("OLLAMA_SCHED_SPREAD")
	// IntelGPU enables experimental Intel GPU detection.
	IntelGPU = Bool("OLLAMA_INTEL_GPU")
	// MultiUserCache optimizes prompt caching for multi-user scenarios.
	MultiUserCache = Bool("OLLAMA_MULTIUSER_CACHE")
	// NewEngine enables the new Ollama engine.
	NewEngine = Bool("OLLAMA_NEW_ENGINE")
	// ContextLength sets the default context length.
	ContextLength = Uint("OLLAMA_CONTEXT_LENGTH", 2048)
)

func String(s string) func() string {
	return func() string {
		v := Var(s)
		slog.Debug("String: reading", "key", s, "value", v)
		return v
	}
}

var (
	LLMLibrary = String("OLLAMA_LLM_LIBRARY")

	CudaVisibleDevices    = String("CUDA_VISIBLE_DEVICES")
	HipVisibleDevices     = String("HIP_VISIBLE_DEVICES")
	RocrVisibleDevices    = String("ROCR_VISIBLE_DEVICES")
	GpuDeviceOrdinal      = String("GPU_DEVICE_ORDINAL")
	HsaOverrideGfxVersion = String("HSA_OVERRIDE_GFX_VERSION")
)

func Uint(key string, defaultValue uint) func() uint {
	return func() uint {
		if s := Var(key); s != "" {
			slog.Debug("Uint: reading", "key", key, "raw", s)
			if n, err := strconv.ParseUint(s, 10, 64); err != nil {
				slog.Warn("Uint: invalid environment variable, using default", "key", key, "value", s, "default", defaultValue)
			} else {
				slog.Debug("Uint: parsed value", "key", key, "value", n)
				return uint(n)
			}
		}
		slog.Debug("Uint: key not set, using default", "key", key, "default", defaultValue)
		return defaultValue
	}
}

var (
	// NumParallel sets the number of parallel model requests.
	NumParallel = Uint("OLLAMA_NUM_PARALLEL", 0)
	// MaxRunners sets the maximum number of loaded models.
	MaxRunners = Uint("OLLAMA_MAX_LOADED_MODELS", 0)
	// MaxQueue sets the maximum number of queued requests.
	MaxQueue = Uint("OLLAMA_MAX_QUEUE", 512)
	// MaxVRAM sets a maximum VRAM override in bytes.
	MaxVRAM = Uint("OLLAMA_MAX_VRAM", 0)
)

func Uint64(key string, defaultValue uint64) func() uint64 {
	return func() uint64 {
		if s := Var(key); s != "" {
			slog.Debug("Uint64: reading", "key", key, "raw", s)
			if n, err := strconv.ParseUint(s, 10, 64); err != nil {
				slog.Warn("Uint64: invalid environment variable, using default", "key", key, "value", s, "default", defaultValue)
			} else {
				slog.Debug("Uint64: parsed value", "key", key, "value", n)
				return n
			}
		}
		slog.Debug("Uint64: key not set, using default", "key", key, "default", defaultValue)
		return defaultValue
	}
}

// GpuOverhead reserves a portion of VRAM per GPU (in bytes).
var GpuOverhead = Uint64("OLLAMA_GPU_OVERHEAD", 0)

type EnvVar struct {
	Name        string
	Value       any
	Description string
}

func AsMap() map[string]EnvVar {
	ret := map[string]EnvVar{
		"OLLAMA_DEBUG":             {"OLLAMA_DEBUG", Debug(), "Show additional debug information (e.g. OLLAMA_DEBUG=1)"},
		"OLLAMA_FLASH_ATTENTION":   {"OLLAMA_FLASH_ATTENTION", FlashAttention(), "Enabled flash attention"},
		"OLLAMA_KV_CACHE_TYPE":     {"OLLAMA_KV_CACHE_TYPE", KvCacheType(), "Quantization type for the K/V cache (default: f16)"},
		"OLLAMA_GPU_OVERHEAD":      {"OLLAMA_GPU_OVERHEAD", GpuOverhead(), "Reserve a portion of VRAM per GPU (bytes)"},
		"OLLAMA_HOST":              {"OLLAMA_HOST", Host(), "IP Address for the ollama server (default 127.0.0.1:11434)"},
		"OLLAMA_KEEP_ALIVE":        {"OLLAMA_KEEP_ALIVE", KeepAlive(), "The duration that models stay loaded in memory (default \"5m\")"},
		"OLLAMA_LLM_LIBRARY":       {"OLLAMA_LLM_LIBRARY", LLMLibrary(), "Set LLM library to bypass autodetection"},
		"OLLAMA_LOAD_TIMEOUT":      {"OLLAMA_LOAD_TIMEOUT", LoadTimeout(), "How long to allow model loads to stall before giving up (default \"5m\")"},
		"OLLAMA_MAX_LOADED_MODELS": {"OLLAMA_MAX_LOADED_MODELS", MaxRunners(), "Maximum number of loaded models per GPU"},
		"OLLAMA_MAX_QUEUE":         {"OLLAMA_MAX_QUEUE", MaxQueue(), "Maximum number of queued requests"},
		"OLLAMA_MODELS":            {"OLLAMA_MODELS", Models(), "The path to the models directory"},
		"OLLAMA_NOHISTORY":         {"OLLAMA_NOHISTORY", NoHistory(), "Do not preserve readline history"},
		"OLLAMA_NOPRUNE":           {"OLLAMA_NOPRUNE", NoPrune(), "Do not prune model blobs on startup"},
		"OLLAMA_NUM_PARALLEL":      {"OLLAMA_NUM_PARALLEL", NumParallel(), "Maximum number of parallel requests"},
		"OLLAMA_ORIGINS":           {"OLLAMA_ORIGINS", AllowedOrigins(), "A comma separated list of allowed origins"},
		"OLLAMA_SCHED_SPREAD":      {"OLLAMA_SCHED_SPREAD", SchedSpread(), "Always schedule model across all GPUs"},
		"OLLAMA_MULTIUSER_CACHE":   {"OLLAMA_MULTIUSER_CACHE", MultiUserCache(), "Optimize prompt caching for multi-user scenarios"},
		"OLLAMA_CONTEXT_LENGTH":    {"OLLAMA_CONTEXT_LENGTH", ContextLength(), "Context length to use unless otherwise specified (default: 2048)"},
		"OLLAMA_NEW_ENGINE":        {"OLLAMA_NEW_ENGINE", NewEngine(), "Enable the new Ollama engine"},

		// Informational
		"HTTP_PROXY":  {"HTTP_PROXY", String("HTTP_PROXY")(), "HTTP proxy"},
		"HTTPS_PROXY": {"HTTPS_PROXY", String("HTTPS_PROXY")(), "HTTPS proxy"},
		"NO_PROXY":    {"NO_PROXY", String("NO_PROXY")(), "No proxy"},
	}

	if runtime.GOOS != "windows" {
		ret["http_proxy"] = EnvVar{"http_proxy", String("http_proxy")(), "HTTP proxy"}
		ret["https_proxy"] = EnvVar{"https_proxy", String("https_proxy")(), "HTTPS proxy"}
		ret["no_proxy"] = EnvVar{"no_proxy", String("no_proxy")(), "No proxy"}
	}

	if runtime.GOOS != "darwin" {
		ret["CUDA_VISIBLE_DEVICES"] = EnvVar{"CUDA_VISIBLE_DEVICES", CudaVisibleDevices(), "Set which NVIDIA devices are visible"}
		ret["HIP_VISIBLE_DEVICES"] = EnvVar{"HIP_VISIBLE_DEVICES", HipVisibleDevices(), "Set which AMD devices are visible by numeric ID"}
		ret["ROCR_VISIBLE_DEVICES"] = EnvVar{"ROCR_VISIBLE_DEVICES", RocrVisibleDevices(), "Set which AMD devices are visible by UUID or numeric ID"}
		ret["GPU_DEVICE_ORDINAL"] = EnvVar{"GPU_DEVICE_ORDINAL", GpuDeviceOrdinal(), "Set which AMD devices are visible by numeric ID"}
		ret["HSA_OVERRIDE_GFX_VERSION"] = EnvVar{"HSA_OVERRIDE_GFX_VERSION", HsaOverrideGfxVersion(), "Override the gfx used for all detected AMD GPUs"}
		ret["OLLAMA_INTEL_GPU"] = EnvVar{"OLLAMA_INTEL_GPU", IntelGPU(), "Enable experimental Intel GPU detection"}
	}

	slog.Debug("AsMap: final environment variables map", "keys", func() []string {
		keys := []string{}
		for k := range ret {
			keys = append(keys, k)
		}
		return keys
	}())
	return ret
}

func Values() map[string]string {
	vals := make(map[string]string)
	for k, v := range AsMap() {
		vals[k] = fmt.Sprintf("%v", v.Value)
	}
	slog.Debug("Values: computed values", "values", vals)
	return vals
}

// Var returns an environment variable stripped of leading and trailing quotes or spaces.
// For GPU-related keys, if the trimmed value is either "-1" or empty,
// it is treated as if it were not set (returning "-1" to force CPU-only mode).
func Var(key string) string {
	raw := os.Getenv(key)
	// slog.Debug("Var: raw value", "key", key, "raw", raw)
	// Extra check: if the raw value is exactly "\"\"", treat it as cpu-only mode.
	if raw == "\"\"" {
		slog.Debug("Var: raw value equals literal \"\"; treating as empty", "key", key)
		raw = "-1"
	}
	trimmed := strings.Trim(strings.TrimSpace(raw), "\"'")
	// slog.Debug("Var: trimmed value", "key", key, "trimmed", trimmed)

	// For GPU-related keys, treat both "-1" and "" as CPU-only mode.
	gpuKeys := map[string]bool{
		"CUDA_VISIBLE_DEVICES":     true,
		"HIP_VISIBLE_DEVICES":      true,
		"ROCR_VISIBLE_DEVICES":     true,
		"GPU_DEVICE_ORDINAL":       true,
		"HSA_OVERRIDE_GFX_VERSION": true,
	}
	if gpuKeys[key] && trimmed == "-1" {
		slog.Debug("Var: GPU variable treated as CPU-only", "key", key, "value", trimmed)
		return "-1"
	}
	return trimmed
}
