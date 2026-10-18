package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This endpoint measures the server host, not registered agent hosts. Sampling
// is on demand, shares concurrent work, and caches completed samples briefly.
const systemSampleTTL = 5 * time.Second
const systemProbeTimeout = 3 * time.Second

type systemMetricsRequest struct{}

type systemUsage struct {
	Available    bool     `json:"available"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
	UsedBytes    *uint64  `json:"used_bytes,omitempty"`
	TotalBytes   *uint64  `json:"total_bytes,omitempty"`
	Source       string   `json:"source"`
	Reason       string   `json:"reason,omitempty"`
}

type systemSnapshot struct {
	SampledAt time.Time   `json:"sampled_at"`
	Platform  string      `json:"platform"`
	CPU       systemUsage `json:"cpu"`
	GPU       systemUsage `json:"gpu"`
	Memory    systemUsage `json:"memory"`
	Disk      systemUsage `json:"disk"`
}

type systemMonitor struct {
	mu      sync.Mutex
	current systemSnapshot
	flight  chan struct{}
	collect func(context.Context) systemSnapshot
	now     func() time.Time
}

func newSystemMonitor(dataDir string) *systemMonitor {
	return &systemMonitor{
		collect: func(ctx context.Context) systemSnapshot { return collectSystem(ctx, dataDir) },
		now:     time.Now,
	}
}

func (m *systemMonitor) snapshot(ctx context.Context) systemSnapshot {
	if ctx.Err() != nil {
		return unavailableSnapshot("probe_failed")
	}
	m.mu.Lock()
	if !m.current.SampledAt.IsZero() && m.now().Sub(m.current.SampledAt) < systemSampleTTL {
		value := m.current
		m.mu.Unlock()
		return value
	}
	flight := m.flight
	if flight == nil {
		flight = make(chan struct{})
		m.flight = flight
		go m.sample(flight)
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return unavailableSnapshot("probe_failed")
	case <-flight:
		m.mu.Lock()
		value := m.current
		m.mu.Unlock()
		return value
	}
}

func (m *systemMonitor) sample(flight chan struct{}) {
	// A caller can stop waiting without cancelling a sample shared by others.
	probeCtx, cancel := context.WithTimeout(context.Background(), systemProbeTimeout)
	value := m.collect(probeCtx)
	cancel()
	value.SampledAt = m.now().UTC()
	m.mu.Lock()
	m.current = value
	close(flight)
	m.flight = nil
	m.mu.Unlock()
}

func unavailableUsage(source, reason string) systemUsage {
	return systemUsage{Source: source, Reason: reason}
}

func unavailableSnapshot(reason string) systemSnapshot {
	usage := unavailableUsage("", reason)
	return systemSnapshot{Platform: runtime.GOOS, CPU: usage, GPU: usage, Memory: usage, Disk: usage}
}

func percentUsage(source string, percent float64) systemUsage {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
		return unavailableUsage(source, "probe_failed")
	}
	percent = math.Round(percent*10) / 10
	return systemUsage{Available: true, UsagePercent: &percent, Source: source}
}

func capacityUsage(source string, total, available uint64) systemUsage {
	if total == 0 || available > total {
		return unavailableUsage(source, "probe_failed")
	}
	used := total - available
	value := percentUsage(source, float64(used)/float64(total)*100)
	value.TotalBytes = &total
	value.UsedBytes = &used
	return value
}

func collectSystem(ctx context.Context, dataDir string) systemSnapshot {
	value := unavailableSnapshot("unsupported_platform")
	if runtime.GOOS == "windows" {
		return collectWindowsSystem(ctx, dataDir)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return value
	}
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		if runtime.GOOS == "darwin" {
			out, err := systemCommand(ctx, nil, "/usr/bin/top", "-l", "2", "-s", "1", "-n", "0")
			value.CPU = parseDarwinCPU(out)
			if err != nil {
				value.CPU = unavailableUsage("top", "probe_failed")
			}
		} else {
			value.CPU = linuxCPU(ctx)
		}
	}()
	go func() {
		defer wg.Done()
		if runtime.GOOS == "darwin" {
			total, totalErr := systemCommand(ctx, nil, "/usr/sbin/sysctl", "-n", "hw.memsize")
			pages, pagesErr := systemCommand(ctx, nil, "/usr/bin/vm_stat")
			value.Memory = parseDarwinMemory(total, pages)
			if totalErr != nil || pagesErr != nil {
				value.Memory = unavailableUsage("sysctl + vm_stat", "probe_failed")
			}
		} else {
			out, _ := os.ReadFile("/proc/meminfo")
			value.Memory = parseLinuxMemory(string(out))
		}
	}()
	go func() {
		defer wg.Done()
		path, err := filepath.Abs(dataDir)
		if err != nil {
			value.Disk = unavailableUsage("df", "probe_failed")
			return
		}
		out, err := systemCommand(ctx, nil, "df", "-Pk", path)
		value.Disk = parseDiskUsage(out)
		if err != nil {
			value.Disk = unavailableUsage("df", "probe_failed")
		}
	}()
	go func() {
		defer wg.Done()
		if runtime.GOOS == "darwin" {
			out, err := systemCommand(ctx, nil, "/usr/sbin/ioreg", "-r", "-c", "IOAccelerator", "-l")
			value.GPU = parseDarwinGPU(out)
			if err != nil {
				value.GPU = unavailableUsage("ioreg", "probe_failed")
			}
		} else {
			value.GPU = linuxGPU(ctx)
		}
	}()
	wg.Wait()
	return value
}

// Commands use no shell, have a shared deadline, a bounded output buffer, and a
// fixed locale. Missing tools or unsupported driver counters are unavailable.
func systemCommand(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(append(os.Environ(), "LC_ALL=C", "LANG=C"), env...)
	cmd.WaitDelay = 100 * time.Millisecond
	output := &systemOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if output.overflow {
		return "", errors.New("system probe output exceeded limit")
	}
	return output.value.String(), nil
}

type systemOutput struct {
	value    strings.Builder
	overflow bool
}

func (b *systemOutput) Write(p []byte) (int, error) {
	const maxProbeBytes = 1 << 20
	n := len(p)
	if n > maxProbeBytes-b.value.Len() {
		p = p[:maxProbeBytes-b.value.Len()]
		b.overflow = true
	}
	b.value.Write(p)
	return n, nil
}

var darwinIdle = regexp.MustCompile(`CPU usage:[^\n]*?([0-9]+(?:\.[0-9]+)?)% idle`)

func parseDarwinCPU(output string) systemUsage {
	matches := darwinIdle.FindAllStringSubmatch(output, -1)
	// The initial top sample is not an interval measurement.
	if len(matches) < 2 {
		return unavailableUsage("top", "probe_failed")
	}
	idle, err := strconv.ParseFloat(matches[len(matches)-1][1], 64)
	if err != nil {
		return unavailableUsage("top", "probe_failed")
	}
	return percentUsage("top", 100-idle)
}

var darwinPageSize = regexp.MustCompile(`page size of ([0-9]+) bytes`)

func parseDarwinMemory(totalOutput, pagesOutput string) systemUsage {
	const source = "sysctl + vm_stat"
	total, err := strconv.ParseUint(strings.TrimSpace(totalOutput), 10, 64)
	pageSize := darwinPageSize.FindStringSubmatch(pagesOutput)
	if err != nil || len(pageSize) != 2 {
		return unavailableUsage(source, "probe_failed")
	}
	size, err := strconv.ParseUint(pageSize[1], 10, 64)
	if err != nil || size == 0 {
		return unavailableUsage(source, "probe_failed")
	}
	pages := map[string]uint64{}
	for _, line := range strings.Split(pagesOutput, "\n") {
		name, raw, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		count, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(raw), "."), 10, 64)
		if err == nil {
			pages[name] = count
		}
	}
	free, hasFree := pages["Pages free"]
	speculative, hasSpeculative := pages["Pages speculative"]
	if !hasFree || !hasSpeculative || free > total/size || speculative > total/size-free {
		return unavailableUsage(source, "probe_failed")
	}
	// This capacity measure includes cached/inactive pages as used memory.
	return capacityUsage(source, total, (free+speculative)*size)
}

var darwinGPU = regexp.MustCompile(`"Device Utilization %"\s*=\s*([0-9]+(?:\.[0-9]+)?)`)

func parseDarwinGPU(output string) systemUsage {
	values := []string{}
	for _, match := range darwinGPU.FindAllStringSubmatch(output, -1) {
		values = append(values, match[1])
	}
	return busiestGPU("ioreg", values)
}

func parseDiskUsage(output string) systemUsage {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i := 1; i+3 < len(fields); i++ {
			if !strings.HasSuffix(fields[i+3], "%") {
				continue
			}
			total, totalErr := strconv.ParseUint(fields[i], 10, 64)
			used, usedErr := strconv.ParseUint(fields[i+1], 10, 64)
			_, availableErr := strconv.ParseUint(fields[i+2], 10, 64)
			if totalErr == nil && usedErr == nil && availableErr == nil && used <= total && total <= math.MaxUint64/1024 {
				return capacityUsage("df", total*1024, (total-used)*1024)
			}
		}
	}
	return unavailableUsage("df", "probe_failed")
}

type cpuTicks struct{ total, idle uint64 }

func parseLinuxCPU(output string) (cpuTicks, bool) {
	fields := strings.Fields(strings.SplitN(output, "\n", 2)[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTicks{}, false
	}
	var ticks cpuTicks
	for i := 1; i < len(fields) && i <= 8; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil || n > math.MaxUint64-ticks.total {
			return cpuTicks{}, false
		}
		ticks.total += n
		if i == 4 || i == 5 {
			ticks.idle += n
		}
	}
	// Guest counters are already included in user/nice and must not be added.
	return ticks, true
}

func linuxCPU(ctx context.Context) systemUsage {
	const source = "/proc/stat"
	before, err := os.ReadFile(source)
	if err != nil {
		return unavailableUsage(source, "probe_failed")
	}
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return unavailableUsage(source, "probe_failed")
	case <-timer.C:
	}
	after, err := os.ReadFile(source)
	first, firstOK := parseLinuxCPU(string(before))
	last, lastOK := parseLinuxCPU(string(after))
	if err != nil || !firstOK || !lastOK || last.total <= first.total || last.idle < first.idle || last.idle-first.idle > last.total-first.total {
		return unavailableUsage(source, "probe_failed")
	}
	return percentUsage(source, 100*(1-float64(last.idle-first.idle)/float64(last.total-first.total)))
}

func parseLinuxMemory(output string) systemUsage {
	values := map[string]uint64{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil && value <= math.MaxUint64/1024 {
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	total, totalOK := values["MemTotal"]
	available, availableOK := values["MemAvailable"]
	if !totalOK || !availableOK {
		return unavailableUsage("/proc/meminfo", "probe_failed")
	}
	return capacityUsage("/proc/meminfo", total, available)
}

func linuxGPU(ctx context.Context) systemUsage {
	paths, _ := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
	values := []string{}
	for _, path := range paths {
		out, err := os.ReadFile(path)
		if err == nil {
			values = append(values, strings.TrimSpace(string(out)))
		}
	}
	if len(values) > 0 {
		return busiestGPU("drm", values)
	}
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return unavailableUsage("drm / nvidia-smi", "not_exposed")
	}
	output, err := systemCommand(ctx, nil, "nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
	if err != nil {
		return unavailableUsage("nvidia-smi", "probe_failed")
	}
	return busiestGPU("nvidia-smi", strings.Split(strings.TrimSpace(output), "\n"))
}

// When several GPU devices expose counters, report the busiest device.
func busiestGPU(source string, values []string) systemUsage {
	var maximum float64
	found := false
	for _, raw := range values {
		value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100 {
			maximum = math.Max(maximum, value)
			found = true
		}
	}
	if !found {
		return unavailableUsage(source, "not_exposed")
	}
	return percentUsage(source, maximum)
}

func collectWindowsSystem(ctx context.Context, dataDir string) systemSnapshot {
	// PowerShell and CIM are native Windows facilities. GPU utilization is not
	// synthesized from engine counters, whose aggregation varies by driver.
	const script = `$ErrorActionPreference='Stop'; $os=Get-CimInstance Win32_OperatingSystem; $cpu=Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'"; $cpuValue=$null; if($null -ne $cpu.PercentProcessorTime){$cpuValue=[double]$cpu.PercentProcessorTime}; $item=Get-Item -LiteralPath $env:AGENT_POST_DATA_DIR; $drive=[System.IO.DriveInfo]::new($item.PSDrive.Root); @{cpu=$cpuValue; memory_total=[uint64]$os.TotalVisibleMemorySize*1024; memory_available=[uint64]$os.FreePhysicalMemory*1024; disk_total=[uint64]$drive.TotalSize; disk_available=[uint64]$drive.TotalFreeSpace} | ConvertTo-Json -Compress`
	output, err := systemCommand(ctx, []string{"AGENT_POST_DATA_DIR=" + dataDir}, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	value := parseWindowsSystem(output)
	if err != nil {
		value = unavailableSnapshot("probe_failed")
		value.CPU.Source, value.Memory.Source, value.Disk.Source = "CIM", "CIM", "DriveInfo"
	}
	value.Platform = "windows"
	value.GPU = unavailableUsage("", "not_exposed")
	return value
}

func parseWindowsSystem(output string) systemSnapshot {
	value := unavailableSnapshot("probe_failed")
	value.Platform = "windows"
	var in struct {
		CPU             *float64 `json:"cpu"`
		MemoryTotal     *uint64  `json:"memory_total"`
		MemoryAvailable *uint64  `json:"memory_available"`
		DiskTotal       *uint64  `json:"disk_total"`
		DiskAvailable   *uint64  `json:"disk_available"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(output, "\ufeff")), &in) != nil {
		return value
	}
	if in.CPU != nil {
		value.CPU = percentUsage("CIM", *in.CPU)
	}
	if in.MemoryTotal != nil && in.MemoryAvailable != nil {
		value.Memory = capacityUsage("CIM", *in.MemoryTotal, *in.MemoryAvailable)
	}
	if in.DiskTotal != nil && in.DiskAvailable != nil {
		value.Disk = capacityUsage("DriveInfo", *in.DiskTotal, *in.DiskAvailable)
	}
	return value
}
