package eventbus

import (
	"context"

	"github.com/alanyang/agent-mesh/internal/domain/event"
)

type Handler func(ctx context.Context, e event.Event)

type Subscription interface {
	Unsubscribe()
}

type EventBus interface {
	Publish(ctx context.Context, e event.Event) error
	Subscribe(ctx context.Context, topic event.Type, handler Handler) (Subscription, error)
}
