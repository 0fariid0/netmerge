package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"math"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"net-merge/internal/nics"
)

const destinationWait = 275 * time.Millisecond

type routePreference struct {
	current, candidate string
	samples            int
	revision           uint64
}

type destinationRoute struct {
	done       chan struct{}
	pending    bool
	updated    time.Time
	revision   uint64
	quality    map[string]destinationSample
	preference routePreference
}

type destinationSample struct {
	ip net.IP
	linkQuality
}

// prepareDestination samples only the real destination of a new UDP flow.
// Cache entries and simultaneous batches are bounded. Missing replies fall
// back to the ordinary per-link measurement.
func (s *Scheduler) prepareDestination(ctx context.Context, f Family, dst netip.Addr) {
	if !dst.IsGlobalUnicast() {
		return
	}
	s.mu.Lock()
	if !s.game {
		s.mu.Unlock()
		return
	}
	now := s.now()
	key := dst.Unmap()
	r := s.destinations[key]
	if r == nil {
		if len(s.destinations) >= 48 {
			for addr, old := range s.destinations {
				if !old.pending && now.Sub(old.updated) > 20*time.Second {
					delete(s.destinations, addr)
				}
			}
		}
		if len(s.destinations) >= 48 {
			s.mu.Unlock()
			return
		}
		if s.destinations == nil {
			s.destinations = make(map[netip.Addr]*destinationRoute)
		}
		r = &destinationRoute{quality: make(map[string]destinationSample)}
		s.destinations[key] = r
	}
	if r.pending || now.Sub(r.updated) < 3*time.Second || s.destinationBatches >= 4 {
		s.mu.Unlock()
		return
	}
	var links []probeJob
	for _, sl := range s.slots {
		if ip := sl.ip(f); sl.present && ip != nil && sl.state[f] != stateDown && !now.Before(sl.downUntil[f]) {
			links = append(links, probeJob{sl, f, ip})
		}
	}
	if len(links) < 2 {
		s.mu.Unlock()
		return
	}
	r.done, r.pending = make(chan struct{}), true
	done := r.done
	s.destinationBatches++
	haveSample := !r.updated.IsZero()
	s.mu.Unlock()

	go s.measureDestination(r, key, links)
	if haveSample {
		return
	}
	timer := time.NewTimer(destinationWait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}
}

func (s *Scheduler) measureDestination(r *destinationRoute, dst netip.Addr, links []probeJob) {
	type result struct {
		job probeJob
		rtt time.Duration
		ok  bool
	}
	results := make(chan result, len(links))
	for _, j := range links {
		go func() {
			rtt, ok := echoDestination(j, dst)
			results <- result{j, rtt, ok}
		}()
	}
	batch := make([]result, 0, len(links))
	for range links {
		batch = append(batch, <-results)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, res := range batch {
		key := res.job.sl.key
		prev := r.quality[key]
		if !res.job.ip.Equal(prev.ip) {
			prev = destinationSample{ip: res.job.ip}
		}
		prev.update(res.rtt, res.ok)
		r.quality[key] = prev
	}
	r.updated = s.now()
	r.revision++
	r.pending = false
	s.destinationBatches--
	close(r.done)
}

func echoDestination(j probeJob, dst netip.Addr) (time.Duration, bool) {
	network, proto := "ip4:icmp", 1
	var echo, reply icmp.Type = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply
	if j.f == Fam6 {
		network, proto = "ip6:ipv6-icmp", 58
		echo, reply = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}
	lc := net.ListenConfig{Control: nics.ForceEgress(j.sl.Index, j.f == Fam6)}
	ctx, cancel := context.WithTimeout(context.Background(), destinationWait)
	defer cancel()
	conn, err := lc.ListenPacket(ctx, network, j.ip.String())
	if err != nil {
		return 0, false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(destinationWait * 9 / 10))
	token := make([]byte, 12)
	if _, err = rand.Read(token); err != nil {
		return 0, false
	}
	id, seq := int(binary.BigEndian.Uint16(token)), int(binary.BigEndian.Uint16(token[2:]))
	msg, err := (&icmp.Message{Type: echo, Body: &icmp.Echo{ID: id, Seq: seq, Data: token}}).Marshal(nil)
	if err != nil {
		return 0, false
	}
	start := time.Now()
	if _, err = conn.WriteTo(msg, &net.IPAddr{IP: dst.AsSlice()}); err != nil {
		return 0, false
	}
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, false
		}
		peer, ok := from.(*net.IPAddr)
		if !ok || !peer.IP.Equal(net.IP(dst.AsSlice())) {
			continue
		}
		packet := buf[:n]
		// Some Windows raw IPv4 sockets include the IPv4 header.
		if j.f == Fam4 && len(packet) >= 20 && packet[0]>>4 == 4 {
			header := int(packet[0]&15) * 4
			if header < 20 || header >= len(packet) {
				continue
			}
			packet = packet[header:]
		}
		parsed, err := icmp.ParseMessage(proto, packet)
		if err != nil || parsed.Type != reply {
			continue
		}
		if body, ok := parsed.Body.(*icmp.Echo); ok && body.ID == id && body.Seq == seq && bytes.Equal(body.Data, token) {
			return max(time.Since(start), time.Microsecond), true
		}
	}
}

func chooseStable(pool []*Slot, pref *routePreference, revision uint64, score func(*Slot) float64) *Slot {
	best := pool[0]
	var current *Slot
	for _, sl := range pool {
		if score(sl) < score(best) {
			best = sl
		}
		if sl.key == pref.current {
			current = sl
		}
	}
	if current == nil || math.IsInf(score(current), 1) {
		*pref = routePreference{current: best.key, revision: revision}
		return best
	}
	if best == current || score(current)-score(best) < float64(8*time.Millisecond) {
		pref.candidate, pref.samples, pref.revision = "", 0, revision
		return current
	}
	if pref.revision != revision {
		if pref.candidate != best.key {
			pref.candidate, pref.samples = best.key, 0
		}
		pref.samples++
		pref.revision = revision
	}
	if pref.samples >= 2 {
		*pref = routePreference{current: best.key, revision: revision}
		return best
	}
	return current
}

// LatestGameRoute describes the last selection, including why a link won.
func (s *Scheduler) LatestGameRoute() GameRoute {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastGameRoute
}

type GameRoute struct {
	Adapter, Basis string
	Destination    netip.Addr
	At             time.Time
}
