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
	// drainFor and drainMax bound how long and how much of a client's unread
	// input is discarded after the answer. Closing a socket with unread input
	// resets the connection and the client could lose the answer.
	drainFor = 250 * time.Millisecond
	drainMax = 64 << 10
)

var errListener = errors.New("listener failed")

// server serves one request per connection (U2).
type server struct {
	uid             uint32
	peerUID         func(net.Conn) (uint32, error)
	status          statusSource
	idle            time.Duration
	requestDeadline time.Duration
	maxConnections  int
	maxResponse     int

	active atomic.Int32 // connections being served now
	peak   atomic.Int32 // highest value active has had (read by the tests)

	mu         sync.Mutex
	lastActive time.Time
}

func newServer(o options, uid uint32, status statusSource) *server {
	return &server{
		uid:             uid,
		peerUID:         o.peerUID,
		status:          status,
		idle:            o.idle,
		requestDeadline: o.requestDeadline,
		maxConnections:  o.maxConnections,
		maxResponse:     o.maxResponse,
	}
}

func (s *server) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

func (s *server) wake() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastActive.Add(s.idle)
}

// serve accepts connections until the helper has been idle for s.idle (nil),
// the listener is closed (nil) or accepting fails (errListener). At most
// s.maxConnections connections are served at once: the next one is not
// accepted before a slot is free, so it waits in the socket backlog.
//
// Idle means no connection accepted and none being served. A connection that
// is being served keeps the helper alive; a transaction will, too (U1).
func (s *server) serve(ln *net.UnixListener) error {
	slots := make(chan struct{}, s.maxConnections)
	var wg sync.WaitGroup
	defer wg.Wait()
	s.touch()
	for {
		slots <- struct{}{}
		ln.SetDeadline(s.wake())
		conn, err := ln.Accept()
		if err != nil {
			<-slots
			switch {
			case errors.Is(err, os.ErrDeadlineExceeded):
				if s.active.Load() == 0 {
					return nil
				}
				s.touch()
				continue
			case errors.Is(err, net.ErrClosed):
				return nil
			}
			return errListener
		}
		n := s.active.Add(1)
		for p := s.peak.Load(); n > p && !s.peak.CompareAndSwap(p, n); p = s.peak.Load() {
		}
		s.touch()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				s.active.Add(-1)
				s.touch()
				<-slots
			}()
			s.handle(conn)
		}()
	}
}

// handle serves one connection. A peer with another uid, or one whose uid
// cannot be read, is closed without an answer (U2).
func (s *server) handle(conn net.Conn) {
	defer conn.Close()
	if uid, err := s.peerUID(conn); err != nil || uid != s.uid {
		return
	}
	conn.SetReadDeadline(time.Now().Add(s.requestDeadline))
	var out []byte
	if line, err := readRequest(conn); err != nil {
		out = errorBody(codeBadRequest)
	} else {
		out = s.respond(line)
	}
	conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	if _, err := conn.Write(out); err != nil {
		return
	}
	drain(conn)
}

// respond answers one request line. Only `status` is answered. stage,
// activate, discard and ack are in the closed list of U2 but are not
// implemented in G6.3a, so they get bad_request like an unknown op; they move
// to their own handlers in G6.5 and G6.6.
func (s *server) respond(line []byte) []byte {
	req, err := decodeRequest(line)
	if err != nil {
		return errorBody(codeBadRequest)
	}
	switch *req.Op {
	case opStatus:
		body, code := s.status.body()
		if code != "" {
			return errorBody(code)
		}
		if len(body) > s.maxResponse {
			return errorBody(codeInternal)
		}
		return body
	case opStage, opActivate, opDiscard, opAck:
		return errorBody(codeBadRequest)
	default:
		return errorBody(codeBadRequest)
	}
}

// drain finishes the exchange: it signals the end of the answer, then reads
// and discards what the client still sends for a short, bounded time, so that
// the close does not reset the connection and cost the client the answer.
func drain(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		c.CloseWrite()
	}
	conn.SetReadDeadline(time.Now().Add(drainFor))
	io.CopyN(io.Discard, conn, drainMax)
}
