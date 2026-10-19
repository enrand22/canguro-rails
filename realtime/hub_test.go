package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/enrand22/canguro-rails/realtime"
)

// subscribe opens a socket for the given channels and waits until the hub has
// registered it, so the tests never race the handshake.
func subscribe(t *testing.T, srv *httptest.Server, hub *realtime.Hub, channels ...string) (*websocket.Conn, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?channels=" + strings.Join(channels, ",")
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		registered := true
		for _, ch := range channels {
			if hub.Connections(ch) == 0 {
				registered = false
			}
		}
		if registered {
			return ws, func() { _ = ws.Close(websocket.StatusNormalClosure, "") }
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("connection was never registered")
	return nil, nil
}

func newHub() *realtime.Hub {
	return realtime.New(realtime.Options{
		Authenticate: func(r *http.Request) (realtime.Subject, error) {
			channels := strings.Split(r.URL.Query().Get("channels"), ",")
			if len(channels) == 1 && channels[0] == "" {
				return realtime.Subject{}, errors.New("no channels")
			}
			return realtime.Subject{ID: "test", Channels: channels}, nil
		},
		PingInterval: time.Hour, // keep pings out of the way in tests
	})
}

func TestPublishReachesOnlySubscribers(t *testing.T) {
	hub := newHub()
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	defer hub.Close()

	a, closeA := subscribe(t, srv, hub, "agent:1")
	defer closeA()
	b, closeB := subscribe(t, srv, hub, "agent:2")
	defer closeB()

	if n := hub.Publish("agent:1", map[string]any{"type": "print_ticket", "job_id": "job_1"}); n != 1 {
		t.Fatalf("delivered = %d, want 1", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, raw, err := a.Read(ctx)
	if err != nil {
		t.Fatalf("subscriber read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if got["job_id"] != "job_1" || got["type"] != "print_ticket" {
		t.Fatalf("payload = %v", got)
	}

	// The other channel must not receive it.
	short, cancelShort := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelShort()
	if _, _, err := b.Read(short); err == nil {
		t.Fatal("message leaked to another channel")
	}
}

func TestPublishReturnsSubscriberCount(t *testing.T) {
	hub := newHub()
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	defer hub.Close()

	_, close1 := subscribe(t, srv, hub, "kitchen")
	defer close1()
	_, close2 := subscribe(t, srv, hub, "kitchen")
	defer close2()

	if n := hub.Publish("kitchen", map[string]any{"type": "new_order"}); n != 2 {
		t.Fatalf("delivered = %d, want 2", n)
	}
	if n := hub.Publish("nobody", map[string]any{"x": 1}); n != 0 {
		t.Fatalf("empty channel delivered = %d, want 0", n)
	}
}

func TestHandshakeRejectedWithoutAuth(t *testing.T) {
	hub := realtime.New(realtime.Options{
		Authenticate: func(r *http.Request) (realtime.Subject, error) {
			return realtime.Subject{}, errors.New("bad token")
		},
	})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	defer hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err == nil {
		t.Fatal("handshake succeeded without auth")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestCloseEndsConnections(t *testing.T) {
	hub := newHub()
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()

	ws, _ := subscribe(t, srv, hub, "agent:9")
	hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := ws.Read(ctx); err == nil {
		t.Fatal("connection survived hub.Close")
	}
}

// A client that cannot keep up is dropped instead of blocking Publish: fill the
// kernel buffer with big messages the client never reads, and the hub must close
// it rather than grow memory.
func TestSlowClientIsDropped(t *testing.T) {
	hub := realtime.New(realtime.Options{
		Authenticate: func(r *http.Request) (realtime.Subject, error) {
			return realtime.Subject{ID: "slow", Channels: []string{"c"}}, nil
		},
		Buffer:       1,
		PingInterval: time.Hour,
	})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()
	defer hub.Close()

	ws, _ := subscribe(t, srv, hub, "c")
	_ = ws

	payload := map[string]any{"blob": strings.Repeat("x", 32<<10)}
	deadline := time.Now().Add(5 * time.Second)
	for i := 0; i < 500 && time.Now().Before(deadline); i++ {
		hub.Publish("c", payload)
		if hub.Connections("c") == 0 {
			return // dropped: exactly what we want
		}
	}
	t.Fatal("slow client was not dropped")
}
