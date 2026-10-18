package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func assertSystemPercent(t *testing.T, got systemUsage, want float64) {
	t.Helper()
	if !got.Available || got.UsagePercent == nil || *got.UsagePercent != want || got.Reason != "" {
		t.Fatalf("want available %g%%, got %+v", want, got)
	}
}

func TestSystemUsageParsers(t *testing.T) {
	t.Run("macOS CPU interval", func(t *testing.T) {
		out := "CPU usage: 10.0% user, 10.0% sys, 80.0% idle\nCPU usage: 12.0% user, 13.0% sys, 75.0% idle\n"
		assertSystemPercent(t, parseDarwinCPU(out), 25)
		if parseDarwinCPU("CPU usage: 10% user, 90% idle").Available || parseDarwinCPU(out+"CPU usage: 101% idle\n").Available {
			t.Fatal("missing interval or invalid idle counter accepted")
		}
	})
	t.Run("macOS memory page size", func(t *testing.T) {
		pages := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 100.\nPages speculative: 50.\nPages inactive: 200.\n"
		value := parseDarwinMemory("16384000\n", pages)
		assertSystemPercent(t, value, 85)
		if *value.UsedBytes != 13926400 || *value.TotalBytes != 16384000 {
			t.Fatal(value)
		}
		for _, invalid := range []string{"", strings.ReplaceAll(pages, "100.", "2000."), strings.ReplaceAll(pages, "Pages speculative", "unknown")} {
			if parseDarwinMemory("16384000", invalid).Available {
				t.Fatal("invalid VM counters accepted")
			}
		}
	})
	t.Run("disk uses occupied capacity", func(t *testing.T) {
		// Available space can exclude reservations; used is the occupied capacity.
		value := parseDiskUsage("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/example 1000 400 500 45% /example path\n")
		assertSystemPercent(t, value, 40)
		if *value.TotalBytes != 1024000 || *value.UsedBytes != 409600 {
			t.Fatal(value)
		}
		for _, invalid := range []string{"", "/dev/example 0 0 0 0% /", "/dev/example 100 200 0 200% /", "/dev/example 18446744073709551615 5 5 1% /"} {
			if parseDiskUsage(invalid).Available {
				t.Fatal("invalid disk capacity accepted")
			}
		}
	})
	t.Run("Linux CPU excludes duplicate guest accounting", func(t *testing.T) {
		value, ok := parseLinuxCPU("cpu 10 20 30 400 50 6 7 8 9 10\ncpu0 1 2 3 4\n")
		if !ok || value.total != 531 || value.idle != 450 {
			t.Fatal(value, ok)
		}
		for _, invalid := range []string{"", "cpu0 1 2 3 4", "cpu 1 2 3", "cpu 1 2 x 4", "cpu 18446744073709551615 2 3 4"} {
			if _, ok := parseLinuxCPU(invalid); ok {
				t.Fatal("invalid CPU counters accepted")
			}
		}
	})
	t.Run("Linux memory uses available capacity", func(t *testing.T) {
		value := parseLinuxMemory("MemTotal: 10000 kB\nMemAvailable: 2500 kB\nMemFree: 1000 kB\n")
		assertSystemPercent(t, value, 75)
		if *value.UsedBytes != 7680000 {
			t.Fatal(value)
		}
		if parseLinuxMemory("MemTotal: 100 kB").Available || parseLinuxMemory("MemTotal: 100 kB\nMemAvailable: 200 kB").Available {
			t.Fatal("missing or invalid memory capacity accepted")
		}
	})
	t.Run("GPU uses busiest exposed device", func(t *testing.T) {
		assertSystemPercent(t, parseDarwinGPU(`"Device Utilization %"=0, "Device Utilization %"=42`), 42)
		assertSystemPercent(t, busiestGPU("test", []string{"0", "N/A", "NaN", "-1", "101"}), 0)
		if value := parseDarwinGPU(`"Renderer Utilization %"=42`); value.Available || value.Reason != "not_exposed" || value.UsagePercent != nil {
			t.Fatal("missing device utilization was fabricated", value)
		}
	})
	t.Run("Windows CIM and drive capacity", func(t *testing.T) {
		value := parseWindowsSystem(`{"cpu":25,"memory_total":10000,"memory_available":2500,"disk_total":100000,"disk_available":60000}`)
		assertSystemPercent(t, value.CPU, 25)
		assertSystemPercent(t, value.Memory, 75)
		assertSystemPercent(t, value.Disk, 40)
		if parseWindowsSystem(`{}`).CPU.Available || parseWindowsSystem(`{"cpu":null}`).CPU.Available || parseWindowsSystem(`broken`).Memory.Available {
			t.Fatal("missing counters accepted")
		}
	})
}

func TestSystemSnapshotCacheAndConcurrentCollection(t *testing.T) {
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	monitor := &systemMonitor{now: func() time.Time { return now }, collect: func(ctx context.Context) systemSnapshot {
		if calls.Add(1) == 1 {
			close(started)
			<-finish
		}
		return systemSnapshot{CPU: percentUsage("test", 25)}
	}}
	var wg sync.WaitGroup
	results := make(chan systemSnapshot, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- monitor.snapshot(context.Background()) }()
	}
	<-started
	close(finish)
	wg.Wait()
	close(results)
	for value := range results {
		assertSystemPercent(t, value.CPU, 25)
		if value.SampledAt != now {
			t.Fatal(value.SampledAt)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent requests did not share a sample", calls.Load())
	}
	monitor.snapshot(context.Background())
	if calls.Load() != 1 {
		t.Fatal("fresh sample was recollected")
	}
	now = now.Add(systemSampleTTL)
	monitor.snapshot(context.Background())
	if calls.Load() != 2 {
		t.Fatal("expired sample was not refreshed")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if monitor.snapshot(cancelled).CPU.Available || calls.Load() != 2 {
		t.Fatal("cancelled request triggered collection")
	}
}

func TestAdminSystemAuthenticationAndStateIsolation(t *testing.T) {
	f := setup(t)
	var calls atomic.Int32
	started, finish := make(chan struct{}), make(chan struct{})
	f.s.system = &systemMonitor{now: time.Now, collect: func(ctx context.Context) systemSnapshot {
		calls.Add(1)
		close(started)
		<-finish
		return systemSnapshot{CPU: percentUsage("test", 12.5)}
	}}
	for _, key := range []string{"", f.keys["alice"], "wrong-password"} {
		if code, _ := f.call("GET", "/admin/api/system", key, nil); code != 401 {
			t.Fatal("unauthenticated system access", code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unauthenticated request probed the host")
	}
	before, _ := json.Marshal(f.s.st)
	response := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/admin/api/system", nil)
	request.Header.Set("Authorization", "Bearer "+testPassword)
	done := make(chan struct{})
	go func() { f.s.Handler().ServeHTTP(response, request); close(done) }()
	<-started
	if !f.s.mu.TryLock() {
		close(finish)
		<-done
		t.Fatal("host sampling held the message-state mutex")
	}
	f.s.mu.Unlock()
	close(finish)
	<-done
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"usage_percent":12.5`) {
		t.Fatal(response.Code, response.Body.String())
	}
	if code, _ := f.call("POST", "/admin/api/system", testPassword, nil); code != 404 {
		t.Fatal("system metrics accepted a mutation", code)
	}
	login := httptest.NewRequest("POST", "/admin/api/login", strings.NewReader(`{"password":"`+testPassword+`"}`))
	loginResponse := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(loginResponse, login)
	request = httptest.NewRequest("GET", "/admin/api/system", nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	response = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(response, request)
	if response.Code != 200 || calls.Load() != 1 {
		t.Fatal("cookie authentication or sample caching failed", response.Code, calls.Load())
	}
	after, _ := json.Marshal(f.s.st)
	if string(before) != string(after) {
		t.Fatal("system inspection mutated application state")
	}
}

func TestSystemCollectionSurvivesInitiatingRequestCancellation(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	monitor := &systemMonitor{now: time.Now, collect: func(ctx context.Context) systemSnapshot {
		calls.Add(1)
		close(started)
		select {
		case <-ctx.Done():
			return unavailableSnapshot("probe_failed")
		case <-finish:
			return systemSnapshot{CPU: percentUsage("test", 25)}
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	initiator := make(chan systemSnapshot, 1)
	go func() { initiator <- monitor.snapshot(ctx) }()
	<-started
	cancel()
	if (<-initiator).CPU.Available {
		t.Fatal("cancelled caller continued waiting")
	}
	follower := make(chan systemSnapshot, 1)
	go func() { follower <- monitor.snapshot(context.Background()) }()
	close(finish)
	assertSystemPercent(t, (<-follower).CPU, 25)
	assertSystemPercent(t, monitor.snapshot(context.Background()).CPU, 25)
	if calls.Load() != 1 {
		t.Fatal("request cancellation invalidated shared collection", calls.Load())
	}
}

func TestSystemCommandProbeHelper(t *testing.T) {
	switch os.Getenv("AGENT_POST_PROBE_HELPER") {
	case "locale":
		fmt.Print(os.Getenv("LC_ALL"))
	case "overflow":
		fmt.Print(strings.Repeat("x", 2<<20))
	case "wait":
		time.Sleep(time.Minute)
	default:
		return
	}
	os.Exit(0)
}

func TestSystemCommandBounds(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	for _, mode := range []string{"locale", "overflow", "wait"} {
		t.Run(mode, func(t *testing.T) {
			// Race-instrumented subprocesses delay exit by one second.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "wait" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 50*time.Millisecond)
				defer stop()
			}
			output, err := systemCommand(ctx, []string{"AGENT_POST_PROBE_HELPER=" + mode}, executable, "-test.run=^TestSystemCommandProbeHelper$")
			if mode == "locale" {
				if err != nil || output != "C" {
					t.Fatal("locale was not fixed", output, err)
				}
			} else if err == nil {
				t.Fatal("unbounded probe accepted", mode)
			}
		})
	}
}
