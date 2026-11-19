package tray

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"net-merge/internal/engine"
	"net-merge/internal/platform"
)

const (
	gameReportFile = "netmerge-game-report.txt"
	loadTestFile   = "netmerge-load-test.txt"
	maxGameRecord  = 2 * time.Hour
)

type matchRecord struct {
	start   time.Time
	mode    string
	links   map[string]*matchLink
	routes  map[string]int
	routeAt time.Time
}

type matchLink struct {
	samples        []time.Duration
	probeID        [2]uint64
	misses, spikes int
	capTicks       int
	capMin, capMax float64
}

func (m *matchRecord) sample(links []engine.SlotStats, route engine.GameRoute) {
	for _, sl := range links {
		x := m.links[sl.Name]
		if x == nil {
			x = &matchLink{}
			m.links[sl.Name] = x
		}
		if sl.ProbeSequence != 0 && sl.ProbeSequence != x.probeID[sl.ProbeFamily] {
			x.probeID[sl.ProbeFamily] = sl.ProbeSequence
			if sl.ProbeOK {
				if sl.ProbeRTT > 0 && sl.ProbeLast-sl.ProbeRTT >= 25*time.Millisecond {
					x.spikes++
				}
				x.samples = append(x.samples, sl.ProbeLast)
			} else {
				x.misses++
			}
		}
		if sl.GameDownMbps > 0 || sl.GameUpMbps > 0 {
			x.capTicks++
			for _, cap := range []float64{sl.GameDownMbps, sl.GameUpMbps} {
				if cap > 0 {
					if x.capMin == 0 {
						x.capMin = cap
					}
					x.capMin = math.Min(x.capMin, cap)
					x.capMax = math.Max(x.capMax, cap)
				}
			}
		}
	}
	if !route.At.Before(m.start) && route.At.After(m.routeAt) {
		m.routeAt = route.At
		m.routes[fmt.Sprintf("%s | %s | %s", route.Destination, route.Adapter, route.Basis)]++
	}
}

func (m *matchRecord) text(end time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mode: %s | Start: %s | Duration: %s\n",
		m.mode, m.start.Format(time.RFC3339), end.Sub(m.start).Round(time.Second))
	for _, name := range sortedNames(m.links) {
		x := m.links[name]
		fmt.Fprintf(&b, "\n%s\n  RTT median: %s | P95: %s | replies: %d | failed probes: %d | spikes: %d\n",
			name, rttText(engine.Percentile(x.samples, 50)), rttText(engine.Percentile(x.samples, 95)),
			len(x.samples), x.misses, x.spikes)
		if x.capTicks > 0 {
			fmt.Fprintf(&b, "  Auto-cap observations: %d | observed range: %.1f to %.1f Mbps\n", x.capTicks, x.capMin, x.capMax)
		}
	}
	for _, path := range sortedNames(m.routes) {
		fmt.Fprintf(&b, "Path observed: %s (%d snapshots with a new selection)\n", path, m.routes[path])
	}
	return b.String()
}

func sortedNames[V any](values map[string]V) []string {
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func rttText(rtt time.Duration) string {
	if rtt <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f ms", float64(rtt)/float64(time.Millisecond))
}

func loadTestText(results []engine.LoadTestResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NetMerge - sampled under-load test\n%s\n\n", time.Now().Format(time.RFC3339))
	b.WriteString("Transfers: Cloudflare speed test. RTT: TCP connect to Cloudflare DNS IP.\n" +
		"Run with other large downloads stopped. Loaded latency is approximate when the byte budget ends quickly.\n")
	if len(results) == 0 {
		b.WriteString("\nNo adapters were available.\n")
	}
	for _, link := range results {
		fmt.Fprintf(&b, "\n%s\n", link.Adapter)
		idle := engine.Percentile(link.Idle.RTT, 50)
		for i, phase := range []engine.LoadPhase{link.Idle, link.Download, link.Upload} {
			label := []string{"Idle", "Download", "Upload"}[i]
			rtt := engine.Percentile(phase.RTT, 50)
			fmt.Fprintf(&b, "  %-8s median %s | P95 %s | replies %d | failed %d",
				label, rttText(rtt), rttText(engine.Percentile(phase.RTT, 95)), len(phase.RTT), phase.Failures)
			if i > 0 {
				fmt.Fprintf(&b, " | %.2f Mbps | %.2f MiB", phase.Mbps(), float64(phase.Bytes)/(1<<20))
				if idle > 0 && len(phase.RTT) >= 3 {
					fmt.Fprintf(&b, " | RTT change %+.1f ms", float64(rtt-idle)/float64(time.Millisecond))
				} else {
					b.WriteString(" | insufficient samples for comparison")
				}
			}
			if phase.Error != "" {
				fmt.Fprintf(&b, " | %s", phase.Error)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (u *trayUI) openDiagnostics(name string) {
	path := filepath.Join(u.stateDir, name)
	if _, err := os.Stat(path); err != nil {
		u.lastErr = u.text("No saved report", "گزارشی ذخیره نشده")
		return
	}
	if err := platform.OpenURL(path); err != nil {
		u.lastErr = err.Error()
	}
}
