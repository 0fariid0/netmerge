package engine

import (
	"net/netip"
	"testing"
	"time"

	"net-merge/internal/nics"
)

func TestGameSteersNewUDPAndBulk(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("fast-download", "127.0.0.2", 3), nic("stable-game", "127.0.0.3", 1)})
	fast, stable := s.slots[0], s.slots[1]
	fast.quality[Fam4] = linkQuality{rtt: 20 * time.Millisecond, jitter: 25 * time.Millisecond, sampled: true}
	stable.quality[Fam4] = linkQuality{rtt: 45 * time.Millisecond, jitter: 2 * time.Millisecond, sampled: true}
	s.SetState(fast, Fam4, true)
	s.SetState(stable, Fam4, true)
	s.SetDistribution("game")

	if got := s.acquireFor(Fam4, nil, true); got != stable {
		t.Fatalf("expected stable game link, got %v", got)
	}
	stable.lastGame.Store(time.Now().UnixNano())
	if got := s.AcquireFor(Fam4, nil); got != fast {
		t.Fatalf("expected bulk flow on other link, got %v", got)
	}
	s.SetState(stable, Fam4, false)
	if got := s.acquireFor(Fam4, nil, true); got != fast {
		t.Fatalf("expected new game flow to avoid broken link, got %v", got)
	}
	s.Release(stable)
	s.Release(fast)
	s.Release(fast)
}

func TestGameCapReservesAndClearsHeadroom(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("shared", "127.0.0.2", 1)})
	sl := s.slots[0]
	s.SetDistribution("game")
	now := time.Now()
	sl.lastGame.Store(now.UnixNano())
	sl.rateIn[Fam4] = 10 * bytesPerMbit
	sl.rateOut[Fam4] = 2 * bytesPerMbit
	sl.setLimit(6)
	s.updateGameCaps(sl, now, 1)
	if p := sl.gameCap.Load(); p == nil || p.inMbps != 8 || p.outMbps != 1.6 {
		t.Fatalf("initial auto cap: %+v", p)
	}
	s.SetState(sl, Fam4, true)
	sl.quality[Fam4] = linkQuality{rtt: 90 * time.Millisecond, base: 20 * time.Millisecond, sampled: true, last: 90 * time.Millisecond, lastOK: true}
	sl.probeSequence[Fam4]++
	sl.rateOut[Fam4] = 30_000
	s.updateGameCaps(sl, now.Add(3*time.Second), 1)
	if p := sl.gameCap.Load(); p == nil || p.inMbps != 6.4 || p.outMbps != 1.6 {
		t.Fatalf("loaded auto cap: %+v", p)
	}
	s.updateGameCaps(sl, now.Add(gameIdle+time.Second), 1)
	if sl.gameCap.Load() != nil || sl.limitMbps() != 6 {
		t.Fatal("temporary cap should expire while the manual cap remains")
	}
}

func TestStableDestinationSwitchAndImmediateUnavailableFallback(t *testing.T) {
	a, b := &Slot{key: "wifi"}, &Slot{key: "usb"}
	pool := []*Slot{a, b}
	pref := &routePreference{}
	scores := map[*Slot]float64{a: float64(20 * time.Millisecond), b: float64(40 * time.Millisecond)}
	score := func(sl *Slot) float64 { return scores[sl] }
	if got := chooseStable(pool, pref, 1, score); got != a {
		t.Fatal("initial choice should use the better link")
	}
	scores[b] = float64(5 * time.Millisecond)
	for _, revision := range []uint64{2, 2} {
		if got := chooseStable(pool, pref, revision, score); got != a {
			t.Fatal("queries must not count as extra probe samples")
		}
	}
	if got := chooseStable(pool, pref, 3, score); got != b {
		t.Fatal("two samples with a substantial improvement should switch the link")
	}
	if got := chooseStable(pool[:1], pref, 3, score); got != a {
		t.Fatal("an unavailable link should be dropped immediately")
	}
}

func TestGamePrefersComparableDestinationThenFallsBack(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("wifi", "127.0.0.2", 1), nic("usb", "127.0.0.3", 1)})
	a, b := s.slots[0], s.slots[1]
	s.SetDistribution("game")
	s.SetState(a, Fam4, true)
	s.SetState(b, Fam4, true)
	a.quality[Fam4].update(10*time.Millisecond, true)
	b.quality[Fam4].update(30*time.Millisecond, true)
	dst := netip.MustParseAddr("192.0.2.1")
	pathSamples := map[string]destinationSample{
		a.key: {ip: a.ip(Fam4), linkQuality: linkQuality{rtt: 60 * time.Millisecond, sampled: true}},
		b.key: {ip: b.ip(Fam4), linkQuality: linkQuality{rtt: 30 * time.Millisecond, sampled: true}},
	}
	s.destinations = map[netip.Addr]*destinationRoute{dst: {updated: time.Now(), quality: pathSamples}}
	if got := s.acquireDestination(Fam4, nil, true, dst); got != b || s.LatestGameRoute().Basis != "destination" {
		t.Fatal("the game route should follow destination samples when both adapters answer")
	}
	s.Release(b)
	delete(pathSamples, b.key)
	if got := s.acquireDestination(Fam4, nil, true, dst); got != a || s.LatestGameRoute().Basis != "general" {
		t.Fatal("a single destination reply should fall back to the general measurements")
	}
	s.Release(a)
}

func TestGameSwitchesOnClearRegressionWithoutWaitingForMoreFlows(t *testing.T) {
	a, b := &Slot{key: "wifi"}, &Slot{key: "usb"}
	pref := &routePreference{current: a.key, revision: 1}
	score := func(sl *Slot) float64 {
		if sl == a {
			return float64(180 * time.Millisecond)
		}
		return float64(45 * time.Millisecond)
	}
	if got := chooseStable([]*Slot{a, b}, pref, 2, score); got != b {
		t.Fatal("clear degradation left the stale route preferred")
	}
}

func TestGameCanUseDestinationReplyDespitePublicProbeFailure(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("wifi", "127.0.0.2", 1), nic("usb", "127.0.0.3", 1)})
	public, direct := s.slots[0], s.slots[1]
	s.SetDistribution("game")
	s.SetState(public, Fam4, true)
	s.SetState(direct, Fam4, false)
	dst := netip.MustParseAddr("192.0.2.1")
	now := s.now()
	s.destinations = map[netip.Addr]*destinationRoute{dst: {updated: now, quality: map[string]destinationSample{
		public.key: {ip: public.ip(Fam4), at: now, linkQuality: linkQuality{failStreak: 2}},
		direct.key: {ip: direct.ip(Fam4), at: now, linkQuality: linkQuality{sampled: true, lastOK: true, rtt: 40 * time.Millisecond}},
	}}}
	got := s.acquireDestination(Fam4, nil, true, dst)
	if got != direct || s.LatestGameRoute().Basis != "destination" {
		t.Fatal("a fresh destination reply should beat repeated misses on the other link")
	}
	s.Release(got)
}
