package engine

import (
	"net/netip"
	"testing"
	"time"

	"net-merge/internal/nics"
)

func TestTCPFailoverPinScopedExpiresAndNotifies(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "127.0.0.2", 1), nic("B", "127.0.0.3", 0.1)})
	a, b := s.slots[0], s.slots[1]
	s.SetState(a, Fam4, true)
	s.SetState(b, Fam4, true)
	now := time.Now()
	s.now = func() time.Time { return now }

	notices := s.RouteFailovers()
	target := newTCPEndpoint("Example.test.", 443)
	for range 2 {
		s.rememberTCP(target, Fam4, []*Slot{a}, b, b.ip(Fam4))
	}
	select {
	case event := <-notices:
		if event.Failed != "A" || event.Working != "B" || event.Destination != "example.test:443" {
			t.Fatalf("unexpected notice: %+v", event)
		}
	default:
		t.Fatal("no confirmed failover notice")
	}
	select {
	case <-notices:
		t.Fatal("duplicate notice")
	default:
	}

	check := func(k tcpEndpoint, want *Slot) {
		t.Helper()
		got := s.acquireRoute(Fam4, nil, false, netip.Addr{}, k)
		if got != want {
			t.Fatalf("%s selected %v, want %s", k, got, want.Name)
		}
		s.Release(got)
	}
	check(newTCPEndpoint("example.test", 443), b)
	check(newTCPEndpoint("example.test", 80), a)
	check(newTCPEndpoint("other.test", 443), a)
	s.SetState(b, Fam4, false)
	check(target, a)
	s.SetState(b, Fam4, true)
	now = now.Add(destinationPinTTL)
	check(target, a)
}

func TestTCPFailoverRechecksBeforeExtendingPin(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("primary", "127.0.0.2", 3), nic("secondary", "127.0.0.3", 1)})
	a, b := s.slots[0], s.slots[1]
	now := time.Now()
	s.now = func() time.Time { return now }
	target := newTCPEndpoint("203.0.113.1", 443)
	s.rememberTCP(target, Fam4, []*Slot{a}, b, b.ip(Fam4))
	now = now.Add(destinationPinTTL)
	if got := s.acquireRoute(Fam4, nil, false, netip.Addr{}, target); got != a {
		t.Fatal("expired preference prevented retesting the primary")
	}
	s.Release(a)
	s.rememberTCP(target, Fam4, []*Slot{a}, b, b.ip(Fam4))
	if pin := s.tcpPins[target]; !pin.expires.Equal(now.Add(destinationRepeatTTL)) {
		t.Fatal("recently confirmed repeated failure did not extend the preference")
	}
}
