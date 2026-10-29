package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/model"
)

type Collector struct {
	mu           sync.Mutex
	prevCPUIdle  uint64
	prevCPUTotal uint64
	prevNet []model.NetCounter
 boot string
	prevNetAt    time.Time
}

func New() *Collector { return &Collector{} }

func (c *Collector) Collect() (model.Metric, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := model.Metric{Timestamp: time.Now()}
	var errs []string
	idle, total, err := readCPU()
	if err != nil {
		errs = append(errs, "cpu: "+err.Error())
	} else {
		if c.prevCPUTotal > 0 && total > c.prevCPUTotal {
			dt := total - c.prevCPUTotal
			di := idle - c.prevCPUIdle
			if dt > 0 {
				m.CPUPercent = (1 - float64(di)/float64(dt)) * 100
			}
		}
		c.prevCPUIdle, c.prevCPUTotal = idle, total
	}
	m.Load1, m.Load5, m.Load15, _ = readLoad()
	m.MemUsed, m.MemTotal, m.SwapUsed, m.SwapTotal, err = readMem()
	if err != nil {
		errs = append(errs, "mem: "+err.Error())
	}
	m.DiskUsed, m.DiskTotal, m.DiskAvailable, m.DiskReserved, err = readDisk("/")
	if err != nil {
		errs = append(errs, "disk: "+err.Error())
	}
 if c.boot == "" { b, _ := os.ReadFile("/proc/sys/kernel/random/boot_id"); c.boot = strings.TrimSpace(string(b)) }
 m.NetworkSource, m.NetworkBoot = model.NetworkSource, c.boot
 counters, err := readNetwork("/proc/net/dev", "/sys/class/net")
 if err != nil || c.boot == "" {
  // A missing boundary is unknown, never a plausible zero or all-interface sum.
  c.prevNet = nil; c.prevNetAt = time.Time{}
 } else {
  m.NetworkCounters = counters
  for _, n := range counters { m.RXBytesTotal += n.RX; m.TXBytesTotal += n.TX }
  c.networkSample(&m, counters, time.Now())
 }
	m.UptimeSeconds, _ = readUptime()
	if len(errs) > 0 {
		return m, errors.New(strings.Join(errs, "; "))
	}
	return m, nil
}

func (c *Collector) networkSample(m *model.Metric, counters []model.NetCounter, at time.Time) {
 rx, tx, valid := model.NetworkDelta(c.prevNet, counters)
 d := at.Sub(c.prevNetAt).Seconds()
 // Keep the previous baseline on an early wake; the next normal sample still
 // includes every byte. Never advertise a tiny burst as a one-second rate.
 if valid && d > 0 && d < .75 { return }
 if valid && d >= .75 && d <= 10 {
  m.RXBps=float64(rx)*8/d; m.TXBps=float64(tx)*8/d
  m.NetworkReady=true; m.NetworkSampleSeconds=d
 }
 c.prevNet, c.prevNetAt = counters, at
}

func readCPU() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, 0, fmt.Errorf("empty /proc/stat")
	}
	p := strings.Fields(s.Text())
	if len(p) < 5 || p[0] != "cpu" {
		return 0, 0, fmt.Errorf("invalid cpu line")
	}
	for i := 1; i < len(p); i++ {
		v, e := strconv.ParseUint(p[i], 10, 64)
		if e != nil {
			continue
		}
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	return
}

func readLoad() (float64, float64, float64, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	p := strings.Fields(string(b))
	if len(p) < 3 {
		return 0, 0, 0, fmt.Errorf("invalid loadavg")
	}
	a, _ := strconv.ParseFloat(p[0], 64)
	b5, _ := strconv.ParseFloat(p[1], 64)
	c, _ := strconv.ParseFloat(p[2], 64)
	return a, b5, c, nil
}

func readMem() (used, total, swapUsed, swapTotal uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer f.Close()
	vals := map[string]uint64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		p := strings.Fields(s.Text())
		if len(p) >= 2 {
			k := strings.TrimSuffix(p[0], ":")
			v, _ := strconv.ParseUint(p[1], 10, 64)
			vals[k] = v * 1024
		}
	}
	if e := s.Err(); e != nil {
		return 0, 0, 0, 0, e
	}
	total = vals["MemTotal"]
	avail := vals["MemAvailable"]
	if total >= avail {
		used = total - avail
	}
	swapTotal = vals["SwapTotal"]
	free := vals["SwapFree"]
	if swapTotal >= free {
		swapUsed = swapTotal - free
	}
	if total == 0 {
		err = fmt.Errorf("MemTotal missing")
	}
	return
}

func diskUsage(st syscall.Statfs_t) (used, total, available, reserved uint64) {
 total = st.Blocks * uint64(st.Bsize)
 if st.Blocks >= st.Bfree { used = (st.Blocks-st.Bfree)*uint64(st.Bsize) }
 available = st.Bavail*uint64(st.Bsize)
 if st.Bfree >= st.Bavail { reserved = (st.Bfree-st.Bavail)*uint64(st.Bsize) }
 return
}
func readDisk(path string) (used, total, available, reserved uint64, err error) {
 var st syscall.Statfs_t
 if err = syscall.Statfs(path, &st); err != nil { return }
 used,total,available,reserved=diskUsage(st)
 return
}

func readUptime() (uint64, error) {
	b, e := os.ReadFile("/proc/uptime")
	if e != nil {
		return 0, e
	}
	p := strings.Fields(string(b))
	if len(p) == 0 {
		return 0, fmt.Errorf("invalid uptime")
	}
	v, e := strconv.ParseFloat(p[0], 64)
	return uint64(v), e
}

func SystemInfo() (hostname, osName, arch string, cores int) {
	hostname, _ = os.Hostname()
	arch = runtime.GOARCH
	cores = runtime.NumCPU()
	osName = "Linux"
	if b, e := os.ReadFile("/etc/os-release"); e == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "PRETTY_NAME=") {
				osName = strings.Trim(strings.TrimPrefix(ln, "PRETTY_NAME="), "\"")
				break
			}
		}
	}
	return
}
