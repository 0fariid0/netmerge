package engine

import (
	"context"
	"net/netip"
	"testing"

	"net-merge/internal/nics"
)

func TestSmartLearnsDirectionAndPrefersSpareCapacity(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("fast-download", "127.0.0.2", 1), nic("fast-upload", "127.0.0.3", 1)})
	s.SetAuto(true)
	a, b := s.slots[0], s.slots[1]
	a.capacity[Fam4][0].estimate, a.capacity[Fam4][1].estimate = 100*bytesPerMbit, 3*bytesPerMbit
	b.capacity[Fam4][0].estimate, b.capacity[Fam4][1].estimate = 30*bytesPerMbit, 40*bytesPerMbit
	a.rateIn[Fam4] = 4 * bytesPerMbit
	if got := s.Acquire(nil); got != a {
		t.Fatal("download-heavy traffic should favor the faster download link")
	}
	s.Release(a)
	a.linkRateIn = 97 * bytesPerMbit // traffic outside NetMerge is using the fast link
	a.actF[Fam4].Store(1)
	a.active.Store(1)
	if got := s.Acquire(nil); got != b {
		t.Fatal("an already busy download link should yield to spare capacity")
	}
	s.Release(b)
	s.Release(a)
	a.linkRateIn, a.rateIn[Fam4] = 0, 0
	b.rateOut[Fam4] = 2 * bytesPerMbit
	if got := s.Acquire(nil); got != b {
		t.Fatal("upload-heavy traffic should favor the faster upload link")
	}
	s.Release(b)
}

func TestCapacityReactsToSustainedSlowdownAndIgnoresIdle(t *testing.T) {
	d := directionCapacity{estimate: 100 * bytesPerMbit}
	d.sample(0, 5, true)
	if d.estimate != 100*bytesPerMbit {
		t.Fatal("idle traffic changed the estimate")
	}
	d.sample(35*bytesPerMbit, 1, true)
	if d.estimate < 90*bytesPerMbit {
		t.Fatal("a single low sample caused a sharp drop")
	}
	for range 5 {
		d.sample(35*bytesPerMbit, 1, true)
	}
	if d.estimate > 65*bytesPerMbit {
		t.Fatal("sustained slowdown did not lower the estimate")
	}
}

func TestUDPAssociationAccountsForLifetime(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("loopback", "127.0.0.1", 1)})
	pc, err := NewBalancer(s).ListenUDP(context.Background(), Fam4, 443, netip.MustParseAddr("127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	sl := s.slots[0]
	if sl.active.Load() != 1 || len(sl.flows[Fam4]) != 1 {
		t.Fatal("live UDP association is not counted")
	}
	s.Tick(flowIdleAfter)
	if sl.busy(Fam4) != 0 {
		t.Fatal("idle UDP association counted as busy")
	}
	pc.Close()
	pc.Close()
	if sl.active.Load() != 0 || sl.idleCount[Fam4] != 0 {
		t.Fatal("closing the association should release its counters exactly once")
	}
}

func TestFastDownloadKeepsSlowLinkForUsefulOverflow(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("fast", "127.0.0.2", 1), nic("slow", "127.0.0.3", 1)})
	s.SetDistribution("fast-download")
	fast, slow := s.slots[0], s.slots[1]
	fast.capacity[Fam4][0].estimate = 100 * bytesPerMbit
	slow.capacity[Fam4][0].estimate = 5 * bytesPerMbit
	for _, load := range []float64{0, 60, 91} {
		fast.linkRateIn = load * bytesPerMbit
		if got := s.Acquire(nil); got != fast {
			t.Fatalf("fast link at %.0f Mbps should still have room", load)
		}
		s.Release(fast)
	}
	fast.linkRateIn = 99 * bytesPerMbit
	if got := s.Acquire(nil); got != slow {
		t.Fatal("useful spare capacity on the slower link should be used under load")
	}
	s.Release(slow)
	slow.linkRateIn = 4.99 * bytesPerMbit
	if got := s.Acquire(nil); got != fast {
		t.Fatal("slow link with no useful headroom should not receive overflow")
	}
	s.Release(fast)
	s.SetState(fast, Fam4, false)
	s.SetState(slow, Fam4, true)
	if got := s.Acquire(nil); got != slow {
		t.Fatal("healthy slow link must still be used when the primary fails")
	}
	s.Release(slow)
}
