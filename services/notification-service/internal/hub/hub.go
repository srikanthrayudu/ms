// Package hub is the in-memory fan-out pub/sub hub that lets the gateway
// subscribe to live notifications for a specific user over SSE.
package hub

import (
	"sync"

	notificationv1 "ms/proto/gen/notification/v1"
)

const bufferSize = 32

// Hub manages per-user subscriber channels.
type Hub struct {
	mu   sync.RWMutex
	subs map[string][]chan *notificationv1.Notification
}

// New creates a Hub.
func New() *Hub {
	return &Hub{subs: make(map[string][]chan *notificationv1.Notification)}
}

// Subscribe registers a channel for the given userID.
// Call the returned unsub function to remove the subscription and close the channel.
func (h *Hub) Subscribe(userID string) (<-chan *notificationv1.Notification, func()) {
	ch := make(chan *notificationv1.Notification, bufferSize)
	h.mu.Lock()
	h.subs[userID] = append(h.subs[userID], ch)
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		list := h.subs[userID]
		for i, c := range list {
			if c == ch {
				h.subs[userID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(h.subs[userID]) == 0 {
			delete(h.subs, userID)
		}
		close(ch)
	}
	return ch, unsub
}

// Publish sends a notification to all subscribers of userID.
// It is non-blocking: a full channel simply misses the message.
func (h *Hub) Publish(userID string, n *notificationv1.Notification) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs[userID] {
		select {
		case ch <- n:
		default:
		}
	}
}
