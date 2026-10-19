// Package realtime pushes messages to connected clients over WebSocket.
//
// It exists because some flows (a POS ticket, a kitchen order) cannot wait for
// the next poll. Polling stays as the fallback — the durable copy lives in the
// database — but the socket is the fast path.
//
// The hub is transport only: it knows nothing about tickets, users or tenants.
// An Authenticate callback decides who may connect and which channels the
// connection subscribes to; Publish sends one JSON message to every connection
// subscribed to a channel.
//
// No envelope is added: the caller's value is marshalled and sent as-is, so a
// client receives exactly the object the app published.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Subject is an authenticated connection: who it is and what it listens to.
type Subject struct {
	ID       string
	Channels []string
}

// Authenticate decides whether a request may open a socket. Returning an error
// rejects the handshake with 401 before the upgrade.
type Authenticate func(r *http.Request) (Subject, error)

// Options configures a Hub. The zero value is usable: 32 buffered messages,
// a ping every 30s and 10s to answer it.
type Options struct {
	// Authenticate is required.
	Authenticate Authenticate
	// Buffer is how many messages may wait for a slow client before it is
	// dropped. Dropping is safe by design: the app keeps the durable record and
	// the client catches up over HTTP when it reconnects.
	Buffer int
	// PingInterval defaults to 30s.
	PingInterval time.Duration
	// PingTimeout defaults to 10s.
	PingTimeout time.Duration
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// OriginPatterns is forwarded to websocket.Accept. Empty means same-origin.
	OriginPatterns []string
}

// Hub tracks connections and fans messages out to channels.
type Hub struct {
	opts Options

	mu        sync.RWMutex
	conns     map[*conn]struct{}
	byChannel map[string]map[*conn]struct{}
	closed    bool
}

// New builds a Hub. It panics when no Authenticate is given: a socket server
// without authentication is a bug, not a configuration choice.
func New(opts Options) *Hub {
	if opts.Authenticate == nil {
		panic("realtime: Options.Authenticate is required")
	}
	if opts.Buffer < 0 {
		panic("realtime: Options.Buffer cannot be negative")
	}
	if opts.Buffer == 0 {
		opts.Buffer = 32
	}
	if opts.PingInterval == 0 {
		opts.PingInterval = 30 * time.Second
	}
	if opts.PingTimeout == 0 {
		opts.PingTimeout = 10 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Hub{
		opts:      opts,
		conns:     map[*conn]struct{}{},
		byChannel: map[string]map[*conn]struct{}{},
	}
}

// Handler returns the http.Handler that upgrades and serves the sockets.
func (h *Hub) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, err := h.opts.Authenticate(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: h.opts.OriginPatterns,
		})
		if err != nil {
			// Accept already wrote the response.
			return
		}

		c := &conn{
			ws:      ws,
			subject: subject,
			send:    make(chan []byte, h.opts.Buffer),
			done:    make(chan struct{}),
			hub:     h,
		}
		if !h.add(c) {
			c.close(websocket.StatusGoingAway, "server shutting down")
			return
		}
		defer h.remove(c)

		ctx := r.Context()
		go c.writeLoop(ctx)
		c.readLoop(ctx)
		c.close(websocket.StatusNormalClosure, "")
	})
}

// Publish sends message to every connection subscribed to channel. It returns
// how many sockets the message was queued for; slow clients are dropped instead
// of blocking the caller.
func (h *Hub) Publish(channel string, message any) int {
	data, err := json.Marshal(message)
	if err != nil {
		h.opts.Logger.Error("realtime: marshal", "channel", channel, "err", err)
		return 0
	}

	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return 0
	}
	targets := make([]*conn, 0, len(h.byChannel[channel]))
	for c := range h.byChannel[channel] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	delivered := 0
	var slow []*conn
	for _, c := range targets {
		select {
		case c.send <- data:
			delivered++
		case <-c.done:
			// Already gone; nothing to do.
		default:
			slow = append(slow, c)
		}
	}
	for _, c := range slow {
		h.opts.Logger.Warn("realtime: slow client dropped", "channel", channel, "subject", c.subject.ID)
		c.close(websocket.StatusPolicyViolation, "client too slow")
	}
	return delivered
}

// Connections reports how many sockets are subscribed to channel.
func (h *Hub) Connections(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byChannel[channel])
}

// Close ends every connection and refuses new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	conns := make([]*conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	for _, c := range conns {
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
}

func (h *Hub) add(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.conns[c] = struct{}{}
	for _, ch := range c.subject.Channels {
		if h.byChannel[ch] == nil {
			h.byChannel[ch] = map[*conn]struct{}{}
		}
		h.byChannel[ch][c] = struct{}{}
	}
	return true
}

func (h *Hub) remove(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	for _, ch := range c.subject.Channels {
		if set := h.byChannel[ch]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.byChannel, ch)
			}
		}
	}
}

// conn is one socket. The write loop is the only writer: Publish queues bytes.
type conn struct {
	ws      *websocket.Conn
	subject Subject
	send    chan []byte
	done    chan struct{}
	once    sync.Once
	hub     *Hub
}

func (c *conn) readLoop(ctx context.Context) {
	for {
		// The client is not expected to send anything but pongs; reading is what
		// notices a disconnect (and keeps coder/websocket answering pings).
		if _, _, err := c.ws.Read(ctx); err != nil {
			return
		}
	}
}

func (c *conn) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(c.hub.opts.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case msg := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.ws.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.close(websocket.StatusInternalError, "write failed")
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, c.hub.opts.PingTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.close(websocket.StatusPolicyViolation, "ping timeout")
				return
			}
		}
	}
}

func (c *conn) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		// Unsubscribe immediately: a closed socket must stop counting as a
		// subscriber, even before the handler returns.
		c.hub.remove(c)
		close(c.done)
		// CloseNow, not Close: the graceful close waits for the peer's close
		// frame, and this runs from Publish (a slow client must never block the
		// call that dropped it) and from shutdown (nobody waits for politeness).
		_ = c.ws.CloseNow()
	})
}
