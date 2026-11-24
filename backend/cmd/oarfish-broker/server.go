package main

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	writeDeadline = 5 * time.Second
	// drainFor and drainMax bound how long and how much unread client input
	// is discarded after the answer: closing a socket with unread input resets
	// the connection, and the client could lose the answer.
	drainFor = 250 * time.Millisecond
	drainMax = 4 << 10
)

var errListener = errors.New("listener failed")

// server serves one request per connection (B2).
type server struct {
	uid             uint32
	refuseAll       bool // the user oarfish does not exist (B4)
	peerUID         func(net.Conn) (uint32, error)
	b               *broker
	now             func() time.Time
	idle            time.Duration
	requestDeadline time.Duration
	maxConnections  int

	active atomic.Int32 // connections being served now

	mu         sync.Mutex
	lastActive time.Time
}

func newServer(o options, uid uint32, refuseAll bool, b *broker) *server {
	return &server{
		uid: uid, refuseAll: refuseAll, peerUID: o.peerUID, b: b, now: o.now,
		idle: o.idle, requestDeadline: o.requestDeadline, maxConnections: o.maxConnections,
	}
}

func (s *server) touch() {
	s.mu.Lock()
	s.lastActive = s.now()
	s.mu.Unlock()
}

func (s *server) last() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastActive
}

// mayExit is the idle exit of B8 with D-C89 item 6: s.idle after the last
// connection, with no connection being served, no operation running, an
// empty rate window and no accepted power operation.
func (s *server) mayExit() bool {
	return s.active.Load() == 0 && !s.now().Before(s.last().Add(s.idle)) && s.b.mayExit()
}

// nextWake is when the idle exit is checked next: the end of the idle time or
// the moment the rate window empties, whichever is later; when that has
// passed and something else keeps the broker (a connection, an operation, an
// accepted power operation), one idle period from now.
func (s *server) nextWake() time.Time {
	now := s.now()
	t := s.last().Add(s.idle)
	if u := s.b.windowEmptyAt(); u.After(t) {
		t = u
	}
	if !t.After(now) {
		t = now.Add(s.idle)
	}
	return t
}

// serve accepts connections until the idle exit (nil), until the listener is
// closed (nil), or until accepting fails (errListener). At most
// maxConnections are served at once; the next waits in the socket backlog.
func (s *server) serve(ln *net.UnixListener) error {
	slots := make(chan struct{}, s.maxConnections)
	var wg sync.WaitGroup
	defer wg.Wait()
	s.touch()
	for {
		slots <- struct{}{}
		ln.SetDeadline(s.nextWake())
		conn, err := ln.Accept()
		if err != nil {
			<-slots
			switch {
			case errors.Is(err, os.ErrDeadlineExceeded):
				if s.mayExit() {
					return nil
				}
				continue
			case errors.Is(err, net.ErrClosed):
				return nil
			}
			return errListener
		}
		s.active.Add(1)
		s.touch()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				s.touch()
				s.active.Add(-1)
				<-slots
			}()
			s.handle(conn)
		}()
	}
}

// handle serves one connection. A peer whose uid is not that of oarfish
// (root included), or whose uid cannot be read, is closed without an answer
// and nothing runs (B4). Without the user oarfish every peer is closed.
func (s *server) handle(conn net.Conn) {
	defer conn.Close()
	if s.refuseAll {
		return
	}
	uid, err := s.peerUID(conn)
	if err != nil || uid != s.uid {
		return
	}
	conn.SetReadDeadline(time.Now().Add(s.requestDeadline))
	var out []byte
	if line, err := readRequest(conn); err != nil {
		s.b.logLine(request{}, codeBadRequest)
		out = encode(answer{V: protocolMax, Code: codeBadRequest})
	} else {
		out = s.b.respond(Caller{UID: uid}, line)
	}
	conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	if _, err := conn.Write(out); err != nil {
		return
	}
	drain(conn)
}

// drain signals the end of the answer, then reads and discards what the
// client still sends for a short, bounded time.
func drain(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		c.CloseWrite()
	}
	conn.SetReadDeadline(time.Now().Add(drainFor))
	io.CopyN(io.Discard, conn, drainMax)
}
