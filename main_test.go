package main

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/shirou/gopsutil/v3/host"
)

func testApplication() *application {
	sampler := &cpuSampler{}
	sampler.store([]float64{12.5}, nil)
	return &application{cpu: sampler, diskPath: "/"}
}

func TestRouting(t *testing.T) {
	app := testApplication()
	for _, prefix := range []string{"/", "/api/3/", "/api/4/"} {
		for _, endpoint := range []string{"health", "cpu"} {
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, prefix+endpoint, nil))
			want := http.StatusOK
			if endpoint == "health" {
				want = http.StatusNoContent
			}
			if recorder.Code != want {
				t.Fatalf("%s%s: got %d, want %d", prefix, endpoint, recorder.Code, want)
			}
		}
	}
	for _, path := range []string{"/", "/api/4/missing", "/api/5/cpu"} {
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", path, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/4/cpu", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: got %d, Allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestCPUFailureAndRecovery(t *testing.T) {
	app := testApplication()
	for _, err := range []error{errors.New("read failed"), nil} {
		app.cpu.store(nil, err)
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/4/cpu", nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("missing CPU data: got %d, want 503", recorder.Code)
		}
	}
	app.cpu.store([]float64{42}, nil)
	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/4/cpu", nil))
		var stat CPUStat
		if err := json.Unmarshal(recorder.Body.Bytes(), &stat); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || stat.Total != 42 {
			t.Fatalf("CPU snapshot: status %d, total %v", recorder.Code, stat.Total)
		}
	}
}

func TestConcurrentCPUSnapshot(t *testing.T) {
	sampler := testApplication().cpu
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for sample := 0; sample < 100; sample++ {
				sampler.store([]float64{float64(sample)}, nil)
				stat, err := sampler.read()
				if err != nil || stat.Total < 0 || stat.Total > 100 {
					t.Errorf("invalid CPU sample: %+v, %v", stat, err)
				}
			}
		}()
	}
	workers.Wait()
}

func TestHomepageMetricSchemas(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux host metric integration test")
	}
	app := testApplication()
	cases := map[string][]string{
		"cpu":       {"total"},
		"load":      {"cpucore", "min1", "min5", "min15"},
		"mem":       {"total", "used", "free", "available", "percent"},
		"quicklook": {"cpu", "mem", "load"},
	}
	for endpoint, fields := range cases {
		t.Run(endpoint, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/4/"+endpoint, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("got %d: %s", recorder.Code, recorder.Body.String())
			}
			var stat map[string]float64
			if err := json.Unmarshal(recorder.Body.Bytes(), &stat); err != nil {
				t.Fatal(err)
			}
			for _, field := range fields {
				if _, ok := stat[field]; !ok {
					t.Errorf("missing numeric field %q", field)
				}
			}
		})
	}
}

func TestCollectionFailures(t *testing.T) {
	app := testApplication()
	app.diskPath = filepath.Join(t.TempDir(), "missing")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/4/fs", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing disk: got %d, want 503", recorder.Code)
	}
	if runtime.GOOS != "linux" {
		return
	}
	t.Setenv("HOST_PROC", t.TempDir())
	for _, endpoint := range []string{"mem", "quicklook"} {
		recorder = httptest.NewRecorder()
		app.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/4/"+endpoint, nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("missing proc data for %s: got %d, want 503", endpoint, recorder.Code)
		}
	}
}

func TestSensorCompatibility(t *testing.T) {
	sensors := sensorStats([]host.TemperatureStat{
		{SensorKey: "coretemp_physical_id_0", Temperature: 52},
		{SensorKey: "coretemp_core_0", Temperature: 51, High: 90, Critical: 105},
		{SensorKey: "k10temp_tctl", Temperature: 53},
		{SensorKey: "nvme_composite", Temperature: 40},
	})
	for index, want := range []string{"Package id 0", "Core 0", "Tctl", "nvme_composite"} {
		if sensors[index].Label != want {
			t.Errorf("sensor %d: label %q, want %q", index, sensors[index].Label, want)
		}
	}
	matched := 0
	for _, sensor := range sensors {
		if sensor.Type == "temperature_core" && (strings.HasPrefix(sensor.Label, "Core") || strings.HasPrefix(sensor.Label, "Tctl")) {
			matched++
		}
	}
	if matched != 2 || sensors[3].Type == "temperature_core" {
		t.Fatalf("Homepage CPU sensor filtering failed: %+v", sensors)
	}
	encoded, err := json.Marshal(sensors[1])
	if err != nil {
		t.Fatal(err)
	}
	var sensor map[string]any
	if err := json.Unmarshal(encoded, &sensor); err != nil {
		t.Fatal(err)
	}
	if sensor["type"] != "temperature_core" || sensor["value"] != float64(51) || sensor["warning"] != float64(90) {
		t.Fatalf("incorrect sensor schema: %s", encoded)
	}
	empty, err := json.Marshal(sensorStats(nil))
	if err != nil || string(empty) != "[]" {
		t.Fatalf("empty sensors: %s, %v", empty, err)
	}
}

func TestUptimeFormatting(t *testing.T) {
	for seconds, want := range map[uint64]string{
		0: "0:00:00", 3661: "1:01:01", 86400: "1 day, 0:00:00", 176461: "2 days, 1:01:01",
	} {
		if got := formatUptime(seconds); got != want {
			t.Errorf("uptime %d: got %q, want %q", seconds, got, want)
		}
	}
}

func TestHealthcheck(t *testing.T) {
	server := httptest.NewServer(testApplication())
	if err := checkHealth(server.URL + "/health"); err != nil {
		t.Fatal(err)
	}
	if err := checkHealth(server.URL + "/api/4/cpu"); err == nil {
		t.Fatal("healthcheck accepted HTTP 200 instead of 204")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, server.URL+"/health", http.StatusFound)
	}))
	defer redirect.Close()
	if err := checkHealth(redirect.URL); err == nil {
		t.Fatal("healthcheck accepted a redirect")
	}
	server.Close()
	if err := checkHealth(server.URL + "/health"); err == nil {
		t.Fatal("healthcheck accepted a closed server")
	}
}

func TestJSONEncodingFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, CPUStat{Total: math.NaN()})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("invalid JSON returned %d, want 500", recorder.Code)
	}
}

func TestConfiguredPort(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{value: "", want: 61208},
		{value: "8080", want: 8080},
		{value: "1", want: 1},
		{value: "65535", want: 65535},
	} {
		t.Setenv("STATS_PORT", test.value)
		port, err := configuredPort()
		if err != nil || port != test.want {
			t.Errorf("STATS_PORT=%q: got %d, %v; want %d", test.value, port, err, test.want)
		}
	}
	for _, value := range []string{"0", "-1", "65536", "abc", "8080/tcp", "127.0.0.1:8080"} {
		t.Setenv("STATS_PORT", value)
		if _, err := configuredPort(); err == nil {
			t.Errorf("invalid STATS_PORT=%q was accepted", value)
		}
	}
}
