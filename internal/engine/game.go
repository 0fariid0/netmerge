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
	last              time.Duration
	lastOK            bool
	failStreak        int
	target            int
}

func (q *linkQuality) update(rtt time.Duration, ok bool) {
	q.last, q.lastOK = rtt, ok
	q.failure *= 0.8
	if !ok {
		q.failure += 0.2
		q.failStreak++
		return
	}
	q.failStreak = 0
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
	rtt := q.rtt
	if q.lastOK {
		rtt = max(rtt, (rtt+q.last)/2)
	}
	return float64(rtt+2*q.jitter) + q.failure*float64(500*time.Millisecond)
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

func gameBulkPool(pool []*Slot, family Family, now time.Time, profile trafficProfile) []*Slot {
	protected := false
	for _, sl := range pool {
		if recentGame(sl, now) {
			protected = true
			break
		}
	}
	if !protected || len(pool) == 1 {
		return pool
	}
	direction := 0
	if profile.mix[1] > profile.mix[0] {
		direction = 1
	}
	var alternatives []*Slot
	for _, sl := range pool {
		if recentGame(sl, now) {
			continue
		}
		capacity := sl.measuredCapacity(family, direction)
		if capacity == 0 && sl.busy(family) == 0 || capacity > 0 && sl.transferLoad(direction) < 0.85*capacity {
			alternatives = append(alternatives, sl)
		}
	}
	if len(alternatives) > 0 {
		return alternatives
	}
	return pool
}

// ValidDistribution reports whether mode names a supported routing policy.
func ValidDistribution(mode string) bool {
	switch mode {
	case "smart", "round-robin", "game", "fast-download":
		return true
	default:
		return false
	}
}

// SetDistribution changes only new route choices. Existing sockets stay pinned.
func (s *Scheduler) SetDistribution(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	roundRobin, game := mode == "round-robin", mode == "game"
	if s.roundRobin != roundRobin {
		s.roundRobinLast = [2]uint64{}
	}
	if game != s.game {
		s.lastGameRoute = GameRoute{}
		s.genericPreference = [2]routePreference{}
	}
	s.roundRobin, s.game = roundRobin, game
	s.fastDownload = mode == "fast-download"
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
		sl.gameClean = 0
		sl.gameProbe = sl.probeSequence
		for i, rate := range rates {
			sl.gamePeak[i] = max(rate, sl.gamePeak[i]*math.Exp(-sec/60))
		}
		return
	}
	var inflated bool
	for f, q := range sl.quality {
		inflated = inflated || (sl.state[f] == stateUp && q.sampled && q.lastOK &&
			q.last-q.base > max(20*time.Millisecond, q.base/3))
	}
	seq := sl.probeSequence
	fresh := false
	for f, q := range sl.quality {
		fresh = fresh || (seq[f] != sl.gameProbe[f] && q.lastOK && sl.state[f] == stateUp)
	}
	sl.gameProbe = seq
	if fresh {
		if inflated {
			sl.gameClean = 0
		} else {
			sl.gameClean++
		}
	}
	old := sl.gameCap.Load()
	previous := [2]float64{}
	if old != nil {
		previous = [2]float64{old.inMbps, old.outMbps}
	}
	caps := previous
	pressure := [2]float64{}
	for i, rate := range rates {
		sl.gamePeak[i] = max(rate, sl.gamePeak[i])
		if caps[i] == 0 && sl.gamePeak[i] >= 25_000 {
			caps[i] = sl.gamePeak[i] * gameReserve / bytesPerMbit
		}
		if caps[i] > 0 {
			pressure[i] = rate / (caps[i] * bytesPerMbit)
		}
	}
	for i, cap := range caps {
		if cap == 0 {
			continue
		}
		// RTT is shared, so use observed traffic to attribute pressure to a direction.
		if fresh && inflated && pressure[i] >= max(0.2, pressure[1-i]*0.5) {
			cap = min(cap*0.8, rates[i]*0.90/bytesPerMbit)
		} else if fresh && !inflated && sl.gameClean >= 3 && pressure[i] >= 0.90 &&
			(previous[i] > 0 && now.After(sl.gameRiseAt)) {
			cap = max(cap*1.05, cap+0.1)
		}
		caps[i] = math.Max(0.1, math.Floor(cap*10)/10)
	}
	if fresh && !inflated && sl.gameClean >= 3 && now.After(sl.gameRiseAt) {
		sl.gameRiseAt = now.Add(3 * time.Second)
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
	if old != nil && old.inMbps == p.inMbps {
		p.in = old.in
	}
	if old != nil && old.outMbps == p.outMbps {
		p.out = old.out
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
