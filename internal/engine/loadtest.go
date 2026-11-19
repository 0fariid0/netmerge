package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"net-merge/internal/nics"
)

const loadTestOrigin = "https://speed.cloudflare.com"

type LoadTestResult struct {
	Adapter                string
	Idle, Download, Upload LoadPhase
}

type LoadPhase struct {
	RTT      []time.Duration
	Failures int
	Bytes    int64
	Duration time.Duration
	Error    string
}

func (p LoadPhase) Mbps() float64 {
	if p.Duration <= 0 {
		return 0
	}
	return float64(p.Bytes) / p.Duration.Seconds() / bytesPerMbit
}

// Percentile uses nearest rank and leaves the original samples unchanged.
func Percentile(samples []time.Duration, percent int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ranked := append([]time.Duration(nil), samples...)
	sort.Slice(ranked, func(i, j int) bool { return ranked[i] < ranked[j] })
	return ranked[(len(ranked)*min(100, max(1, percent))+99)/100-1]
}

func (s *Scheduler) TestUnderLoad(ctx context.Context) []LoadTestResult {
	s.mu.Lock()
	var jobs []probeJob
	for _, sl := range s.slots {
		if !sl.present {
			continue
		}
		f := Fam4
		if sl.ip(f) == nil || (sl.state[f] == stateDown && sl.state[Fam6] == stateUp) {
			f = Fam6
		}
		if ip := sl.ip(f); ip != nil {
			jobs = append(jobs, probeJob{sl, f, ip})
		}
	}
	s.mu.Unlock()
	var out []LoadTestResult
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		result := LoadTestResult{Adapter: j.sl.Name}
		network := "tcp4"
		if j.f == Fam6 {
			network = "tcp6"
		}
		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: j.ip}, Control: nics.ForceEgress(j.sl.Index, j.f == Fam6), Timeout: 2 * time.Second}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, target string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, target)
			},
			DisableCompression: true, ResponseHeaderTimeout: 4 * time.Second,
		}
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		probe := func(ctx context.Context) (time.Duration, error) {
			start := time.Now()
			c, err := dialer.DialContext(ctx, network, probeTargets[j.f][0])
			if err == nil {
				c.Close()
			}
			return time.Since(start), err
		}
		for i, phase := range []*LoadPhase{&result.Idle, &result.Download, &result.Upload} {
			*phase = testPhase(ctx, client, probe, i)
		}
		transport.CloseIdleConnections()
		out = append(out, result)
	}
	return out
}

func testPhase(ctx context.Context, client *http.Client, probe func(context.Context) (time.Duration, error), direction int) LoadPhase {
	duration := 8 * time.Second
	if direction == 0 {
		duration = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	results := make(chan LoadPhase, 1)
	var transferred atomic.Int64
	start := time.Now()
	if direction != 0 {
		go func() {
			results <- loadTransfer(ctx, client, direction, &transferred)
		}()
	}
	var phase LoadPhase
	tick := time.NewTicker(225 * time.Millisecond)
	defer tick.Stop()
	var lastBytes int64
	for {
		select {
		case transfer := <-results:
			phase.Bytes, phase.Duration, phase.Error = transfer.Bytes, transfer.Duration, transfer.Error
			return phase
		case <-ctx.Done():
			if direction == 0 {
				phase.Duration = time.Since(start)
				return phase
			}
			transfer := <-results // network operations observe the same context
			phase.Bytes, phase.Duration, phase.Error = transfer.Bytes, transfer.Duration, transfer.Error
			return phase
		case <-tick.C:
			step, stop := context.WithTimeout(ctx, 850*time.Millisecond)
			rtt, err := probe(step)
			stop()
			bytes := transferred.Load()
			progress := bytes > lastBytes
			lastBytes = bytes
			if direction != 0 && !progress {
				continue
			}
			if err == nil {
				phase.RTT = append(phase.RTT, rtt)
			} else if ctx.Err() == nil {
				phase.Failures++
			}
		}
	}
}

func loadTransfer(ctx context.Context, client *http.Client, direction int, transferred *atomic.Int64) LoadPhase {
	const downBytes = 8 << 20
	const upBytes = 1 << 20
	const repeats = 4
	start := time.Now()
	result := LoadPhase{}
	for i := 0; i < repeats && ctx.Err() == nil; i++ {
		method, url := http.MethodGet, fmt.Sprintf("%s/__down?bytes=%d", loadTestOrigin, downBytes)
		var body io.Reader
		if direction == 2 {
			method, url = http.MethodPost, loadTestOrigin+"/__up"
			body = progressReader{bytes.NewReader(make([]byte, upBytes)), transferred}
		}
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			result.Error = err.Error()
			break
		}
		if body != nil {
			req.ContentLength = upBytes
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		resp, err := client.Do(req)
		if err == nil {
			reader := io.LimitReader(resp.Body, downBytes)
			if direction == 1 {
				reader = progressReader{reader, transferred}
			}
			n, readErr := io.Copy(io.Discard, reader)
			resp.Body.Close()
			switch {
			case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent:
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			case direction == 1:
				result.Bytes += n
				err = readErr
			case readErr == nil:
				result.Bytes += upBytes
			default:
				err = readErr
			}
		}
		if err != nil {
			if ctx.Err() == nil || result.Bytes == 0 {
				result.Error = err.Error()
			}
			break
		}
	}
	result.Duration = time.Since(start)
	return result
}

type progressReader struct {
	io.Reader
	bytes *atomic.Int64
}

func (r progressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes.Add(int64(n))
	return n, err
}
