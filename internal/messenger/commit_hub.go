package messenger

import (
	"sync"

	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
)

// commitHub fans out newly committed messages to active Subscribe streams.
// Publish blocks when a subscriber's buffer is full (upstream backpressure).
type commitHub struct {
	mu        sync.Mutex
	byChannel map[string]map[*commitSubscriber]struct{}
}

type commitSubscriber struct {
	ch chan *raftogrampb.StoredMessage
}

func (h *commitHub) register(channelID string, bufferSize int) (<-chan *raftogrampb.StoredMessage, func()) {
	sub := &commitSubscriber{
		ch: make(chan *raftogrampb.StoredMessage, bufferSize),
	}

	h.mu.Lock()
	if h.byChannel == nil {
		h.byChannel = make(map[string]map[*commitSubscriber]struct{})
	}
	if h.byChannel[channelID] == nil {
		h.byChannel[channelID] = make(map[*commitSubscriber]struct{})
	}
	h.byChannel[channelID][sub] = struct{}{}
	h.mu.Unlock()

	unregister := func() {
		h.mu.Lock()
		if subs, ok := h.byChannel[channelID]; ok {
			delete(subs, sub)
			if len(subs) == 0 {
				delete(h.byChannel, channelID)
			}
		}
		h.mu.Unlock()
		close(sub.ch)
	}

	return sub.ch, unregister
}

func (h *commitHub) publish(channelID string, msg *raftogrampb.StoredMessage) {
	h.mu.Lock()
	subs := make([]*commitSubscriber, 0, len(h.byChannel[channelID]))
	for sub := range h.byChannel[channelID] {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.ch <- msg
	}
}
