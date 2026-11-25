// Package services is the service side of the privilege broker for systemd
// services (ADR 0011, step G7.4): the read path R1 (the list, read with the
// two fixed systemctl shapes) and the action job (one start, stop or restart
// at a time, sent to the broker, audited once). It decides nothing the broker
// does not also enforce: the rows and their offered actions are a convenience
// for the page, and the broker's refusal is the boundary.
//
// There is no timer, no polling and no goroutine of its own while idle: a read
// runs only when a request asks for the list, and a job only after a request
// started it.
package services

import (
	"context"

	"github.com/swingstate/oarfish/backend/internal/audit"
	"github.com/swingstate/oarfish/backend/internal/brokerclient"
	"github.com/swingstate/oarfish/backend/internal/clock"
)

// Config configures New.
type Config struct {
	// Broker is the client of the broker's socket. Nil: every action is
	// unavailable.
	Broker brokerclient.API
	// Audit receives the service-action entries (ADR 0009 A7); nil discards them.
	Audit func(audit.Event)
	// Clock is the time source; nil is the system clock.
	Clock clock.Clock
}

// Manager is the read path and the action job together. Its methods are safe
// for concurrent use.
type Manager struct {
	broker  brokerclient.API
	reader  *reader
	actions *actions
}

// New returns a Manager.
func New(cfg Config) *Manager {
	c := cfg.Clock
	if c == nil {
		c = clock.System()
	}
	r := newReader(c)
	return &Manager{broker: cfg.Broker, reader: r, actions: newActions(cfg.Broker, cfg.Audit, c.Now, r.invalidate)}
}

// List returns the service list (cached for 5 s, one read at a time).
func (m *Manager) List(ctx context.Context) (List, *Error) { return m.reader.read(ctx) }

// BrokerAvailable tells whether the broker's socket exists (a stat only).
func (m *Manager) BrokerAvailable() bool { return m.broker != nil && m.broker.Available() }

// Submit starts an action job, or refuses it with unavailable or busy (both
// audited). It returns SubmitAccepted when the job runs.
func (m *Manager) Submit(req Request) string { return m.actions.submit(req) }

// Last returns the last action since the service started, or nil.
func (m *Manager) Last() *ActionView { return m.actions.lastAction() }

// Close ends a running job (it is audited as unavailable) and a running read.
func (m *Manager) Close() {
	m.actions.close()
	m.reader.close()
}
