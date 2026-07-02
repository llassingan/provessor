package sse

import (
	"reflect"
	"sync"
	"time"
)

type EventBroker struct {
	mu      sync.RWMutex
	clients map[string]map[chan SSEEvent]struct{}
	history map[string][]SSEEvent
	maxHist int
}

const defaultMaxHistory = 50

type SSEEvent struct {
	InstanceID string `json:"instance_id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Step       string `json:"step"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	Timestamp  int64  `json:"timestamp"`
}

func NewEventBroker() *EventBroker {
	return &EventBroker{
		clients: make(map[string]map[chan SSEEvent]struct{}),
		history: make(map[string][]SSEEvent),
		maxHist: defaultMaxHistory,
	}
}

func (b *EventBroker) Subscribe(instanceID string) <-chan SSEEvent {
	ch := make(chan SSEEvent, 64)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.clients[instanceID] == nil {
		b.clients[instanceID] = make(map[chan SSEEvent]struct{})
	}
	b.clients[instanceID][ch] = struct{}{}

	return ch
}

func (b *EventBroker) Unsubscribe(instanceID string, ch <-chan SSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if channels, ok := b.clients[instanceID]; ok {
		for c := range channels {
			if reflect.ValueOf(c).Pointer() == reflect.ValueOf(ch).Pointer() {
				delete(channels, c)
				close(c)
				break
			}
		}
		if len(channels) == 0 {
			delete(b.clients, instanceID)
		}
	}
}

func (b *EventBroker) Publish(instanceID string, event SSEEvent) {
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	if event.InstanceID == "" {
		event.InstanceID = instanceID
	}

	b.mu.Lock()
	b.history[instanceID] = append(b.history[instanceID], event)
	if len(b.history[instanceID]) > b.maxHist {
		b.history[instanceID] = b.history[instanceID][len(b.history[instanceID])-b.maxHist:]
	}
	b.mu.Unlock()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients[instanceID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *EventBroker) History(instanceID string) []SSEEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	events := b.history[instanceID]
	if events == nil {
		return nil
	}
	cpy := make([]SSEEvent, len(events))
	copy(cpy, events)
	return cpy
}

func (b *EventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for instanceID, channels := range b.clients {
		for ch := range channels {
			close(ch)
		}
		delete(b.clients, instanceID)
	}
}
