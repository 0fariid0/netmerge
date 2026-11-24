package engine

import (
	"math"
	"time"
)

type directionCapacity struct {
	estimate     float64
	reduced      float64 // seconds of sustained slower traffic
	firstRate    float64
	lastMeasured time.Time
}

func (d *directionCapacity) sample(rate, seconds float64, busy bool) {
	if rate < minMeasuredPeak {
		// Low-volume traffic and idle connections do not define link capacity.
		d.reduced, d.firstRate = 0, 0
		return
	}
	if d.estimate == 0 {
		if d.firstRate == 0 {
			d.firstRate = rate
			return
		}
		d.estimate = min(d.firstRate, rate)
		d.firstRate = 0
		return
	}
	tau := 18.0
	switch {
	case rate >= d.estimate:
		d.reduced = 0
		tau = 2
	case busy && rate < 0.70*d.estimate:
		d.reduced += seconds
		if d.reduced >= 3 {
			tau = 4
		}
	default:
		d.reduced = 0
		if !busy {
			return // a slow server or a short transfer doesn't establish a lower capacity
		}
	}
	d.estimate += (rate - d.estimate) * (1 - math.Exp(-seconds/tau))
}

func (sl *Slot) sampleCapacity(f Family, seconds float64, now time.Time) {
	busy := sl.busy(f) > 1
	rates := [2]float64{sl.rateIn[f], sl.rateOut[f]}
	for i, rate := range rates {
		d := &sl.capacity[f][i]
		if capped := sl.cappedCapacity(i, d.estimate); capped < d.estimate {
			// A capped sample cannot prove the physical link became slower.
			d.reduced = 0
			continue
		}
		d.sample(rate, seconds, busy)
		if rate >= minMeasuredPeak && d.estimate > 0 && rate >= 0.75*d.estimate {
			d.lastMeasured = now
		}
	}
}

type trafficProfile struct {
	maximum [2]float64
	mix     [2]float64
}

func (sl *Slot) cappedCapacity(i int, estimate float64) float64 {
	if estimate == 0 {
		return 0
	}
	if cap := sl.limit.Load(); cap != nil {
		estimate = math.Min(estimate, cap.mbitPerSec*bytesPerMbit)
	}
	if cap := sl.gameCap.Load(); cap != nil {
		rates := [2]float64{cap.inMbps, cap.outMbps}
		if rates[i] > 0 {
			estimate = math.Min(estimate, rates[i]*bytesPerMbit)
		}
	}
	return estimate
}

func (sl *Slot) measuredCapacity(f Family, i int) float64 {
	return sl.cappedCapacity(i, sl.capacity[f][i].estimate)
}

func measureDemand(pool []*Slot, f Family) trafficProfile {
	var p trafficProfile
	var traffic [2]float64
	for _, sl := range pool {
		for i := range p.maximum {
			p.maximum[i] = math.Max(p.maximum[i], sl.measuredCapacity(f, i))
		}
		traffic[0] += sl.rateIn[f]
		traffic[1] += sl.rateOut[f]
	}
	for i, rate := range traffic {
		if p.maximum[i] > 0 {
			// A small floor favors the relevant direction without letting an
			// idle or ACK-only direction dominate new connections.
			p.mix[i] = max(minMeasuredPeak, rate)
		}
	}
	return p
}

func (p trafficProfile) share(sl *Slot, f Family) float64 {
	total := p.mix[0] + p.mix[1]
	if total == 0 {
		return sl.weight[f]
	}
	// Physical adapter counters include other programs and traffic in the
	// other address family. Relayed counters cover systems without OS counters.
	utilization := [2]float64{
		sl.transferLoad(0),
		sl.transferLoad(1),
	}
	var score float64
	for i, mix := range p.mix {
		if mix == 0 {
			continue
		}
		capacity := sl.measuredCapacity(f, i)
		if capacity == 0 {
			// Give an unmeasured link enough traffic to learn its capacity
			// without flattening all previously measured link preferences.
			capacity = sl.cappedCapacity(i, 0.35*p.maximum[i])
		}
		free := max(0, capacity-utilization[i])
		// Keep a soft floor when busy so short bursts and a single active
		// connection don't immediately exclude the fastest link.
		score += mix * (0.30*capacity + 0.70*free) / p.maximum[i]
	}
	return score / total
}

// chooseFastDownload only admits slower links once the leading measured link
// is busy and an alternative has enough headroom to help.
func chooseFastDownload(pool []*Slot, family Family, now time.Time, explore bool) *Slot {
	const busyFraction = 0.85
	const minimumUsefulRate = 0.5 * bytesPerMbit

	var fastest *Slot
	var peak float64
	for _, link := range pool {
		if rate := link.measuredCapacity(family, 0); rate > peak {
			fastest, peak = link, rate
		}
	}
	if fastest == nil {
		// Spread a bounded burst across links while learning download capacity.
		// Recent picks represent work that has not appeared in traffic samples yet.
		candidate := pool[0]
		for _, link := range pool[1:] {
			load := link.busy(family) + int64(link.fastPicks[family])
			candidateLoad := candidate.busy(family) + int64(candidate.fastPicks[family])
			if load < candidateLoad || (load == candidateLoad && link.transferLoad(0) < candidate.transferLoad(0)) {
				candidate = link
			}
		}
		return candidate
	}

	// Even a lightly loaded known link must not starve an unmeasured or
	// stale alternative forever. Only a small share of new flows explores.
	if explore {
		var candidate *Slot
		for _, link := range pool {
			if link == fastest || link.busy(family) != 0 || link.fastPicks[family] != 0 {
				continue
			}
			d := link.capacity[family][0]
			stale := !d.lastMeasured.IsZero() && now.Sub(d.lastMeasured) >= 2*time.Minute
			if d.estimate == 0 || stale {
				if candidate == nil || link.total.Load() < candidate.total.Load() {
					candidate = link
				}
			}
		}
		if candidate != nil {
			return candidate
		}
	}
	primaryLoad := fastest.transferLoad(0)
	if primaryLoad < busyFraction*peak {
		return fastest
	}
	best := fastest
	bestSpare := max(minimumUsefulRate, peak-primaryLoad)
	for _, link := range pool {
		if link == fastest {
			continue
		}
		capacity := link.measuredCapacity(family, 0)
		if capacity == 0 {
			// Sample unknown links opportunistically when the known primary is busy.
			if link.busy(family) > 0 || link.fastPicks[family] > 0 {
				continue
			}
			capacity = link.cappedCapacity(0, peak*0.10)
		}
		// Until the next sample, spreading new overflow across useful links
		// prevents a burst from all seeing the same spare capacity.
		spare := (capacity - link.transferLoad(0)) / float64(1+link.fastPicks[family])
		if spare > bestSpare {
			best, bestSpare = link, spare
		}
	}
	return best
}

func (sl *Slot) transferLoad(direction int) float64 {
	if direction == 1 {
		return max(sl.linkRateOut, sl.rateOut[Fam4]+sl.rateOut[Fam6])
	}
	return max(sl.linkRateIn, sl.rateIn[Fam4]+sl.rateIn[Fam6])
}
