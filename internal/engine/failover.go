package engine

import (
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	destinationPinTTL = 5 * time.Minute
	destinationPinMax = 1024
)

// TCP pins are keyed by service endpoint. Success on one port is not evidence
// that another port on the same host needs the same adapter.
type tcpEndpoint struct {
	host string
	port int
}

func newTCPEndpoint(host string, port int) tcpEndpoint {
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
	}
	return tcpEndpoint{host, port}
}

type tcpPin struct {
	link    *Slot
	source  net.IP
	expires time.Time
}

// RouteFailover is emitted when a failed TCP path was confirmed by a successful
// attempt on another adapter. It describes new connection attempts only.
type RouteFailover struct {
	Destination string
	Failed      string
	Working     string
}

func (s *Scheduler) rememberTCP(endpoint tcpEndpoint, f Family, failed []*Slot, working *Slot, source net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !working.present || !source.Equal(working.ip(f)) {
		return
	}
	now := s.now()
	if s.tcpPins == nil {
		s.tcpPins = make(map[tcpEndpoint]tcpPin)
	}
	prev := s.tcpPins[endpoint]
	if prev.link == working && prev.source.Equal(source) && now.Before(prev.expires) {
		return // avoid duplicate notifications from concurrent retries
	}
	if len(s.tcpPins) >= destinationPinMax {
		var oldest tcpEndpoint
		var earliest time.Time
		for key, pin := range s.tcpPins {
			if earliest.IsZero() || pin.expires.Before(earliest) {
				oldest, earliest = key, pin.expires
			}
		}
		delete(s.tcpPins, oldest)
	}
	s.tcpPins[endpoint] = tcpPin{link: working, source: source, expires: now.Add(destinationPinTTL)}
	if s.routeFailovers != nil {
		names := make([]string, 0, len(failed))
		for _, link := range failed {
			names = append(names, link.Name)
		}
		select {
		case s.routeFailovers <- RouteFailover{endpoint.String(), strings.Join(names, ", "), working.Name}:
		default:
		}
	}
}

func (k tcpEndpoint) String() string {
	return net.JoinHostPort(k.host, strconv.Itoa(k.port))
}

// RouteFailovers subscribes the tray to bounded, non-blocking route notices.
func (s *Scheduler) RouteFailovers() <-chan RouteFailover {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routeFailovers == nil {
		s.routeFailovers = make(chan RouteFailover, 8)
	}
	return s.routeFailovers
}
