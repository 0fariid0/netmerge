package engine

import (
	"math"
	"time"
)

const (
	gameIdle    = 8 * time.Second
	gameReserve = 0.8
)

// Link measurements describe the probe endpoint, not the game server.
type linkQuality struct {
	rtt, jitter, base time.Duration
	failure           float64
	sampled           bool
}

func (q *linkQuality) update(rtt time.Duration, ok bool) {
	q.failure *= 0.8
	if !ok {
		q.failure += 0.2
		return
	}
	if !q.sampled {
		q.rtt, q.base, q.sampled = rtt, rtt, true
		return
	}
	delta := rtt - q.rtt
	q.rtt += delta / 4
	q.jitter += (time.Duration(math.Abs(float64(delta))) - q.jitter) / 4
	// Let a slightly stale baseline recover without treating one loaded sample as idle.
	q.base = min(rtt, q.base+time.Millisecond)
}

func (q linkQuality) score() float64 {
	if !q.sampled {
		return math.Inf(1)
	}
	return float64(q.rtt+2*q.jitter) + q.failure*float64(500*time.Millisecond)
}

// IsLatencySensitiveUDP treats native UDP traffic like games and calls as
// latency-sensitive. DNS and the common HTTP/QUIC port use normal distribution.
func IsLatencySensitiveUDP(port uint16) bool {
	switch port {
	case 0, 53, 123, 443, 853, 5353, 5355:
		return false
	default:
		return true
	}
}

func recentGame(sl *Slot, now time.Time) bool {
	last := sl.lastGame.Load()
	return last != 0 && now.UnixNano()-last <= int64(gameIdle)
}

// SetDistribution changes only new route choices. Existing sockets stay pinned.
func (s *Scheduler) SetDistribution(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	roundRobin, game := mode == "round-robin", mode == "game"
	if s.roundRobin != roundRobin {
		s.next = [2]uint64{}
	}
	s.roundRobin, s.game = roundRobin, game
	if !game {
		for _, sl := range s.slots {
			sl.gameCap.Store(nil)
		}
	}
}

// updateGameCaps reserves headroom for a live game when bulk traffic shares its
// link. Each direction uses its own observed throughput. Manual caps still apply.
func (s *Scheduler) updateGameCaps(sl *Slot, now time.Time, sec float64) {
	rates := [2]float64{sl.rateIn[Fam4] + sl.rateIn[Fam6], sl.rateOut[Fam4] + sl.rateOut[Fam6]}
	for i, rate := range rates {
		game := sl.gameBytes[i].Load()
		rates[i] = max(0, rate-float64(game-sl.gameLastBytes[i])/sec)
		sl.gameLastBytes[i] = game
	}
	if !s.game || !sl.present || !recentGame(sl, now) {
		sl.gameCap.Store(nil)
		sl.gameRiseAt = now.Add(3 * time.Second)
		for i, rate := range rates {
			sl.gamePeak[i] = max(rate, sl.gamePeak[i]*math.Exp(-sec/60))
		}
		return
	}
	loaded := false
	for f, q := range sl.quality {
		loaded = loaded || (sl.state[f] == stateUp && q.sampled && q.rtt-q.base > max(20*time.Millisecond, q.base/3))
	}
	factor := gameReserve
	if loaded {
		factor = 0.65
	}
	old := sl.gameCap.Load()
	tryRaise := !loaded && old != nil && !now.Before(sl.gameRiseAt)
	if tryRaise {
		sl.gameRiseAt = now.Add(3 * time.Second)
	}
	caps := [2]float64{}
	for i, rate := range rates {
		// Do not turn a paced sample into a new lower capacity estimate.
		sl.gamePeak[i] = max(rate, sl.gamePeak[i])
		if tryRaise {
			previous := [2]float64{old.inMbps, old.outMbps}
			if previous[i] > 0 && rate >= 0.92*previous[i]*bytesPerMbit {
				sl.gamePeak[i] *= 1.05
			}
		}
		if sl.gamePeak[i] >= 25_000 {
			// Round to avoid replacing token buckets on every tick.
			caps[i] = math.Max(0.1, math.Floor(sl.gamePeak[i]*factor/bytesPerMbit*10)/10)
		}
	}
	if old != nil && old.inMbps == caps[0] && old.outMbps == caps[1] {
		return
	}
	if caps == [2]float64{} {
		sl.gameCap.Store(nil)
		return
	}
	p := &gameLimit{inMbps: caps[0], outMbps: caps[1]}
	if caps[0] > 0 {
		p.in = newBucket(caps[0] * bytesPerMbit)
	}
	if caps[1] > 0 {
		p.out = newBucket(caps[1] * bytesPerMbit)
	}
	sl.gameCap.Store(p)
}

type gameLimit struct {
	in, out         *bucket
	inMbps, outMbps float64
}

func (sl *Slot) paceBulk(down bool, n int) {
	if p := sl.gameCap.Load(); p != nil {
		b := p.out
		if down {
			b = p.in
		}
		if b != nil {
			b.take(n)
		}
	}
}

func (sl *Slot) noteGame(down bool, n int) {
	if n <= 0 {
		return
	}
	sl.lastGame.Store(time.Now().UnixNano())
	if down {
		sl.gameBytes[0].Add(int64(n))
	} else {
		sl.gameBytes[1].Add(int64(n))
	}
}

func (sl *Slot) bulkChunk(down bool) int {
	chunk := 0
	if p := sl.limit.Load(); p != nil {
		chunk = p.chunk
	}
	if p := sl.gameCap.Load(); p != nil {
		cap := p.outMbps
		if down {
			cap = p.inMbps
		}
		if cap > 0 {
			autoChunk := limitChunkFor(cap * bytesPerMbit)
			if chunk == 0 || autoChunk < chunk {
				chunk = autoChunk
			}
		}
	}
	return chunk
}
