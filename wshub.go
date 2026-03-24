package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/coder/websocket"
)

// WsHub enables broadcasting to a set of subscribers. (for real-time communication between website instances and discord command)
type WsHub struct {

	// broadcastLimiter controls the rate limit applied to the publish endpoint.
	//
	// Defaults to one publish every 100ms with a burst of 8.
	broadcastLimiter *rate.Limiter

	// serveMux routes the various endpoints to the appropriate handler.
	serveMux *http.ServeMux

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{}
}

// newChatServer constructs a chatServer with the defaults.
func WsHubSetup(serveMux *http.ServeMux) *WsHub {
	cs := &WsHub{
		subscribers:      make(map[*subscriber]struct{}),
		broadcastLimiter: rate.NewLimiter(rate.Every(time.Millisecond*100), 8),
		serveMux:         serveMux,
	}
	cs.serveMux.HandleFunc("/api/subscribe", cs.subscribeHandler)
	cs.serveMux.HandleFunc("/api/publish", cs.publishHandler)

	return cs
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, closeSlow is called.
type subscriber struct {
	msgs      chan []byte
	closeSlow func()
}

// subscribeHandler accepts the WebSocket connection and then subscribes
// it to all future messages.
func (cs *WsHub) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	err := cs.subscribe(w, r)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		slog.Error("websocket error", slog.Any("err", err))
		return
	}
}

// publishHandler reads the request body with a limit of 8192 bytes and then publishes
// the received message.
func (cs *WsHub) publishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 8192)
	msg, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}

	cs.publish(msg)

	w.WriteHeader(http.StatusAccepted)
}

// subscribe subscribes the given WebSocket to all broadcast messages.
// It creates a subscriber with a buffered msgs chan to give some room to slower
// connections and then registers the subscriber. It then listens for all messages
// and writes them to the WebSocket. If the context is cancelled or
// an error occurs, it returns and deletes the subscription.
//
// It uses CloseRead to keep reading from the connection to process control
// messages and cancel the context if the connection drops.
func (cs *WsHub) subscribe(w http.ResponseWriter, r *http.Request) error {
	var mu sync.Mutex
	var c *websocket.Conn
	var closed bool
	s := &subscriber{
		msgs: make(chan []byte),
		closeSlow: func() {
			mu.Lock()
			defer mu.Unlock()
			closed = true
			if c != nil {
				c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
			}
		},
	}
	cs.addSubscriber(s)
	defer cs.deleteSubscriber(s)

	c2, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"http://localhost:*",
			"http://127.0.0.1:*",
			"https://localhost:*",
			"https://127.0.0.1:*",
			"https://*.zrok.io",
			"https://*-share.zrok.io",
		},
	})
	if err != nil {
		return err
	}
	mu.Lock()
	if closed {
		mu.Unlock()
		return net.ErrClosed
	}
	c = c2
	mu.Unlock()
	defer c.CloseNow()

	ctx := c.CloseRead(context.Background())

	for {
		select {
		case msg := <-s.msgs:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish publishes the msg to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
func (cs *WsHub) publish(msg []byte) {
	cs.subscribersMu.Lock()
	defer cs.subscribersMu.Unlock()

	cs.broadcastLimiter.Wait(context.Background())

	for s := range cs.subscribers {
		select {
		case s.msgs <- msg:
		default:
			go s.closeSlow()
		}
	}
}

// addSubscriber registers a subscriber.
func (cs *WsHub) addSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	cs.subscribers[s] = struct{}{}
	cs.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (cs *WsHub) deleteSubscriber(s *subscriber) {
	cs.subscribersMu.Lock()
	delete(cs.subscribers, s)
	cs.subscribersMu.Unlock()
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, msg)
}
