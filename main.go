package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

type QuickLook struct {
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Load float64 `json:"load"`
}

type CPUStat struct {
	Total float64 `json:"total"`
}

type LoadStat struct {
	CPUCore int     `json:"cpucore"`
	Min1    float64 `json:"min1"`
	Min5    float64 `json:"min5"`
	Min15   float64 `json:"min15"`
}

type MemStat struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Free      uint64  `json:"free"`
	Available uint64  `json:"available"`
	Percent   float64 `json:"percent"`
}

type SensorStat struct {
	Label    string  `json:"label"`
	Type     string  `json:"type"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Warning  float64 `json:"warning,omitempty"`
	Critical float64 `json:"critical,omitempty"`
}

type DiskStat struct {
	DiskName   string  `json:"disk_name"`
	MountPoint string  `json:"mnt_point"`
	Total      uint64  `json:"size"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

type cpuSampler struct {
	mu   sync.RWMutex
	stat CPUStat
	err  error
}

func (sampler *cpuSampler) store(values []float64, err error) {
	if err == nil && len(values) == 0 {
		err = errors.New("no CPU sample available")
	}
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	sampler.err = err
	if err == nil {
		sampler.stat.Total = values[0]
	}
}

func (sampler *cpuSampler) read() (CPUStat, error) {
	sampler.mu.RLock()
	defer sampler.mu.RUnlock()
	return sampler.stat, sampler.err
}

func (sampler *cpuSampler) run(ctx context.Context) {
	_, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		sampler.store(nil, err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			values, err := cpu.PercentWithContext(ctx, 0, false)
			sampler.store(values, err)
		}
	}
}

func readLoad() (LoadStat, error) {
	average, err := load.Avg()
	if err != nil {
		return LoadStat{}, err
	}
	cores, err := cpu.Counts(true)
	if err != nil {
		return LoadStat{}, err
	}
	return LoadStat{
		CPUCore: cores,
		Min1:    average.Load1,
		Min5:    average.Load5,
		Min15:   average.Load15,
	}, nil
}

func readMemory() (MemStat, error) {
	memory, err := mem.VirtualMemory()
	if err != nil {
		return MemStat{}, err
	}
	if memory.Total == 0 {
		return MemStat{}, errors.New("memory total unavailable")
	}
	return MemStat{
		Total:     memory.Total,
		Used:      memory.Used,
		Free:      memory.Free,
		Available: memory.Available,
		Percent:   memory.UsedPercent,
	}, nil
}

func sensorLabel(sensorKey string) string {
	normalized := strings.ToLower(sensorKey)
	parts := strings.Split(normalized, "_")
	suffix := parts[len(parts)-1]
	switch {
	case strings.Contains(normalized, "physical_id"), strings.Contains(normalized, "package_id"):
		return "Package id " + suffix
	case strings.Contains(normalized, "_core_"):
		return "Core " + suffix
	case strings.HasPrefix(normalized, "cpu_thermal"), strings.HasPrefix(normalized, "cpu-thermal"):
		return "cpu_thermal"
	case strings.Contains(normalized, "tctl"):
		return "Tctl"
	case strings.Contains(normalized, "temperature"):
		return "Temperature"
	default:
		return sensorKey
	}
}

func sensorStats(temperatures []host.TemperatureStat) []SensorStat {
	sensors := make([]SensorStat, 0, len(temperatures))
	for _, temperature := range temperatures {
		key := strings.ToLower(temperature.SensorKey)
		kind := "temperature_other"
		if strings.HasPrefix(key, "coretemp") || strings.HasPrefix(key, "k10temp") ||
			strings.HasPrefix(key, "cpu_thermal") || strings.HasPrefix(key, "cpu-thermal") ||
			strings.HasPrefix(key, "zenpower") {
			kind = "temperature_core"
		}
		sensors = append(sensors, SensorStat{
			Label:    sensorLabel(temperature.SensorKey),
			Type:     kind,
			Value:    temperature.Temperature,
			Unit:     "C",
			Warning:  temperature.High,
			Critical: temperature.Critical,
		})
	}
	return sensors
}

func readSensors() ([]SensorStat, error) {
	temperatures, err := host.SensorsTemperatures()
	if err != nil && len(temperatures) == 0 {
		return nil, err
	}
	return sensorStats(temperatures), nil
}

func readDisk(path string) ([]DiskStat, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return nil, err
	}
	return []DiskStat{{
		DiskName:   "root",
		MountPoint: "/",
		Total:      usage.Total,
		Used:       usage.Used,
		Free:       usage.Free,
		Percent:    usage.UsedPercent,
	}}, nil
}

func formatUptime(seconds uint64) string {
	clock := fmt.Sprintf("%d:%02d:%02d", seconds%86400/3600, seconds%3600/60, seconds%60)
	days := seconds / 86400
	if days == 0 {
		return clock
	}
	if days == 1 {
		return "1 day, " + clock
	}
	return fmt.Sprintf("%d days, %s", days, clock)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "Cannot encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(data, '\n')); err != nil {
		log.Printf("write response: %v", err)
	}
}

type application struct {
	cpu      *cpuSampler
	diskPath string
}

func (app *application) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	path := request.URL.Path
	for _, prefix := range []string{"/api/3/", "/api/4/", "/"} {
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
			break
		}
	}
	var value any
	var err error
	switch path {
	case "health":
		w.WriteHeader(http.StatusNoContent)
		return
	case "cpu":
		value, err = app.cpu.read()
	case "quicklook":
		var processor CPUStat
		var memory MemStat
		var average LoadStat
		processor, err = app.cpu.read()
		if err == nil {
			memory, err = readMemory()
		}
		if err == nil {
			average, err = readLoad()
		}
		value = QuickLook{CPU: processor.Total, Mem: memory.Percent, Load: average.Min1}
	case "load":
		value, err = readLoad()
	case "mem":
		value, err = readMemory()
	case "disk", "fs":
		value, err = readDisk(app.diskPath)
	case "uptime":
		var seconds uint64
		seconds, err = host.Uptime()
		value = formatUptime(seconds)
	case "sensors":
		value, err = readSensors()
	case "system":
		var hostname string
		hostname, err = os.Hostname()
		value = map[string]string{"os_name": runtime.GOOS, "hostname": hostname}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Unknown endpoint"})
		return
	}
	if err != nil {
		log.Printf("collect %s: %v", path, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Metric unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func checkHealth(url string) error {
	client := http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

func configuredPort() (int, error) {
	value := os.Getenv("STATS_PORT")
	if value == "" {
		return 61208, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("STATS_PORT must be an integer between 1 and 65535")
	}
	return port, nil
}

func run(ctx context.Context, port int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	diskPath := os.Getenv("STATS_DISK_PATH")
	if diskPath == "" {
		diskPath = "/"
	}
	sampler := &cpuSampler{err: errors.New("CPU sample warming up")}
	go sampler.run(ctx)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           &application{cpu: sampler, diskPath: diskPath},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	log.Printf("listening on %s", listener.Addr())
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func main() {
	port, err := configuredPort()
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := checkHealth(fmt.Sprintf("http://127.0.0.1:%d/health", port)); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 1 {
		log.Fatal("Usage: stats [healthcheck]")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, port); err != nil {
		log.Fatal(err)
	}
}
