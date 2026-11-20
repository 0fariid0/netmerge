package engine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"

	"net-merge/internal/nics"
)

// slotConn counts a pinned TCP flow's bytes and releases its slot on Close. It exposes
// CloseRead/CloseWrite because the TUN relay half-closes each direction separately.
type slotConn struct {
	*net.TCPConn
	sl       *Slot
	fam      Family
	s        *Scheduler
	once     sync.Once
	activity *flowActivity
}

func (c *slotConn) Read(p []byte) (int, error) {
	if chunk := c.sl.bulkChunk(true); chunk > 0 && len(p) > chunk {
		p = p[:chunk] // hand out a chunk at a time, so a limited flow stays smooth
	}
	n, err := c.TCPConn.Read(p)
	if n > 0 {
		c.sl.bytesIn[c.fam].Add(int64(n))
		if c.activity != nil {
			c.activity.bytes.Add(int64(n))
		}
		c.sl.throttle(true, n)
		c.sl.paceBulk(true, n)
	}
	return n, err
}

func (c *slotConn) Write(p []byte) (int, error) {
	n, err := c.sl.writePaced(c.TCPConn, p, false)
	c.sl.bytesOut[c.fam].Add(int64(n))
	if c.activity != nil {
		c.activity.bytes.Add(int64(n))
	}
	return n, err
}

// release frees the slot's balancing count. It happens at the first half-close, not at the final
// Close: the relay can keep a finished flow open for up to a minute waiting for the peer's FIN,
// and counting those as active would steer new flows away from the NIC that just finished.
func (c *slotConn) release() {
	c.once.Do(func() {
		c.s.untrack(c.sl, c.fam, c.activity)
		c.s.ReleaseFor(c.sl, c.fam)
	})
}

func (c *slotConn) CloseRead() error  { c.release(); return c.TCPConn.CloseRead() }
func (c *slotConn) CloseWrite() error { c.release(); return c.TCPConn.CloseWrite() }
func (c *slotConn) Close() error      { c.release(); return c.TCPConn.Close() }

// DialPinned is DialTCP for front-ends that hand the conn to someone else to pump.
func (b *Balancer) DialPinned(ctx context.Context, host string, port int) (net.Conn, error) {
	c, sl, f, err := b.DialTCP(ctx, host, port)
	if err != nil {
		return nil, err
	}
	return &slotConn{TCPConn: c.(*net.TCPConn), sl: sl, fam: f, s: b.S, activity: b.S.track(sl, f)}, nil
}

type slotPacketConn struct {
	net.PacketConn
	sl               *Slot
	fam              Family
	latencySensitive bool
	scheduler        *Scheduler
	activity         *flowActivity
	once             sync.Once
}

func (c *slotPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, a, err := c.PacketConn.ReadFrom(p)
	if n > 0 {
		c.sl.bytesIn[c.fam].Add(int64(n))
		c.activity.bytes.Add(int64(n))
		if c.latencySensitive {
			c.sl.noteGame(true, n)
		} else {
			c.sl.paceBulk(true, n)
		}
		c.sl.throttlePacket(true, n)
	}
	return n, a, err
}

func (c *slotPacketConn) WriteTo(p []byte, a net.Addr) (int, error) {
	if !c.latencySensitive {
		c.sl.paceBulk(false, len(p))
	}
	c.sl.throttlePacket(false, len(p))
	n, err := c.PacketConn.WriteTo(p, a)
	if c.latencySensitive {
		c.sl.noteGame(false, n)
	}
	c.sl.bytesOut[c.fam].Add(int64(n))
	c.activity.bytes.Add(int64(n))
	return n, err
}

func (c *slotPacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(func() {
		c.scheduler.untrack(c.sl, c.fam, c.activity)
		c.scheduler.ReleaseFor(c.sl, c.fam)
	})
	return err
}

// ListenUDP opens a UDP socket pinned to one NIC (a UDP "flow" = the socket, so every datagram
// of that flow leaves the same way). Active UDP transfers (including QUIC) count
// toward the load; the scheduler discounts idle associations.
func (b *Balancer) ListenUDP(ctx context.Context, f Family, dstPort uint16, dst netip.Addr) (net.PacketConn, error) {
	network := "udp4"
	if f == Fam6 {
		network = "udp6"
	}
	latencySensitive := IsLatencySensitiveUDP(dstPort)
	dst = dst.Unmap()
	if latencySensitive {
		b.S.prepareDestination(ctx, f, dst)
	}
	tried := map[string]struct{}{}
	lastErr := errors.New("no usable network adapter")
	for {
		sl := b.S.acquireDestination(f, tried, latencySensitive, dst)
		if sl == nil {
			return nil, lastErr
		}
		tried[sl.key] = struct{}{}
		lc := net.ListenConfig{Control: nics.ForceEgress(sl.Index, f == Fam6)}
		pc, err := lc.ListenPacket(ctx, network, net.JoinHostPort(sl.ip(f).String(), "0"))
		if err != nil {
			b.S.ReleaseFor(sl, f)
			if classify(err).linkDown {
				b.S.Fail(sl, f, b.FailCooldown)
			}
			lastErr = err
			continue
		}
		return &slotPacketConn{PacketConn: pc, sl: sl, fam: f, latencySensitive: latencySensitive,
			scheduler: b.S, activity: b.S.track(sl, f)}, nil
	}
}
