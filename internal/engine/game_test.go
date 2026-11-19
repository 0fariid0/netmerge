package engine

import (
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
	sl.quality[Fam4] = linkQuality{rtt: 90 * time.Millisecond, base: 20 * time.Millisecond, sampled: true}
	s.updateGameCaps(sl, now.Add(3*time.Second), 1)
	if p := sl.gameCap.Load(); p == nil || p.inMbps != 6.5 || p.outMbps != 1.3 {
		t.Fatalf("loaded auto cap: %+v", p)
	}
	s.updateGameCaps(sl, now.Add(gameIdle+time.Second), 1)
	if sl.gameCap.Load() != nil || sl.limitMbps() != 6 {
		t.Fatal("temporary cap should expire while the manual cap remains")
	}
}
