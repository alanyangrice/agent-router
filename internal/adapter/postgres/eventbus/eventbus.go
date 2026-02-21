package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	porteventbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
)

type EventBus struct {
	pool *pgxpool.Pool

	mu   sync.RWMutex
	subs map[event.Channel]map[*subscription]struct{}
}

func New(pool *pgxpool.Pool) *EventBus {
	return &EventBus{
		pool: pool,
		subs: make(map[event.Channel]map[*subscription]struct{}),
	}
}

// Publish sends an event via Postgres NOTIFY on the domain channel for the event type.
func (eb *EventBus) Publish(ctx context.Context, e event.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	channel := channelName(event.ChannelFor(e.Type))
	_, err = eb.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, string(payload))
	if err != nil {
		return fmt.Errorf("publishing event on channel %s: %w", channel, err)
	}
	return nil
}

// Subscribe starts a background goroutine that LISTENs on the domain Postgres channel
// and invokes handler for every event published to that channel.
func (eb *EventBus) Subscribe(ctx context.Context, ch event.Channel, handler porteventbus.Handler) (porteventbus.Subscription, error) {
	conn, err := eb.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection for LISTEN: %w", err)
	}

	channel := channelName(ch)
	_, err = conn.Exec(ctx, "LISTEN "+channel)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("executing LISTEN on channel %s: %w", channel, err)
	}

	sub := &subscription{
		cancel: func() {},
		done:   make(chan struct{}),
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub.cancel = cancel

	eb.mu.Lock()
	if eb.subs[ch] == nil {
		eb.subs[ch] = make(map[*subscription]struct{})
	}
	eb.subs[ch][sub] = struct{}{}
	eb.mu.Unlock()

	go func() {
		defer func() {
			conn.Exec(context.Background(), "UNLISTEN "+channel)
			conn.Release()
			close(sub.done)
		}()

		for {
			notification, err := conn.Conn().WaitForNotification(subCtx)
			if err != nil {
				if subCtx.Err() != nil {
					return
				}
				continue
			}

			var e event.Event
			if err := json.Unmarshal([]byte(notification.Payload), &e); err != nil {
				continue
			}

			handler(subCtx, e)
		}
	}()

	return sub, nil
}

// channelName converts a domain Channel to a safe Postgres channel identifier.
func channelName(ch event.Channel) string {
	return "agent_mesh_" + string(ch)
}

type subscription struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *subscription) Unsubscribe() {
	s.cancel()
	<-s.done
}
