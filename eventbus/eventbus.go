package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type EventID string

type Event interface {
	EventID() EventID
}

type EventHandler func(ctx context.Context, event Event) error

type Subscription struct {
	eventID EventID
	id      uint64
}

type PublishResult struct {
	Delivered int
	Pending   int
	Errors    []error
	Err       error
}

type HandlerFailure struct {
	Subscription Subscription
	Event        Event
	Err          error
	Panic        any
}

type FailureHook func(HandlerFailure)

type BusSubscriber interface {
	Subscribe(eventID EventID, cb EventHandler) Subscription
	Unsubscribe(id Subscription)
}

type BusPublisher interface {
	PublishSync(ctx context.Context, event Event) PublishResult
	PublishAsync(ctx context.Context, event Event) <-chan PublishResult
}

type Bus interface {
	BusSubscriber
	BusPublisher
}

type Option func(*bus)

func WithPublishTimeout(timeout time.Duration) Option {
	return func(b *bus) {
		b.publishTimeout = timeout
	}
}

func WithFailureHook(hook FailureHook) Option {
	return func(b *bus) {
		b.failureHook = hook
	}
}

func SlogFailureHook(logger *slog.Logger) FailureHook {
	if logger == nil {
		logger = slog.Default()
	}

	return func(failure HandlerFailure) {
		attrs := []any{
			slog.String("event_id", string(failure.Subscription.eventID)),
			slog.Uint64("subscription_id", failure.Subscription.id),
			slog.Any("event", failure.Event),
			slog.Any("err", failure.Err),
		}
		if failure.Panic != nil {
			attrs = append(attrs, slog.Any("panic", failure.Panic))
		}

		logger.Error("event handler failed", attrs...)
	}
}

func New(opts ...Option) Bus {
	b := &bus{
		infos: make(map[EventID]subscriptionInfoList),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b
}

type subscriptionInfo struct {
	id uint64
	cb EventHandler
}

type subscriptionInfoList []*subscriptionInfo

type bus struct {
	lock           sync.Mutex
	nextID         uint64
	publishTimeout time.Duration
	failureHook    FailureHook
	infos          map[EventID]subscriptionInfoList
}

func (bus *bus) Subscribe(eventID EventID, cb EventHandler) Subscription {
	if cb == nil {
		panic("eventbus: nil handler")
	}

	bus.lock.Lock()
	defer bus.lock.Unlock()
	id := bus.nextID
	bus.nextID++
	sub := &subscriptionInfo{
		id: id,
		cb: cb,
	}
	bus.infos[eventID] = append(bus.infos[eventID], sub)
	return Subscription{
		eventID: eventID,
		id:      id,
	}
}

func (bus *bus) Unsubscribe(subscription Subscription) {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	if infos, ok := bus.infos[subscription.eventID]; ok {
		for idx, info := range infos {
			if info.id == subscription.id {
				infos = append(infos[:idx], infos[idx+1:]...)
				break
			}
		}
		if len(infos) == 0 {
			delete(bus.infos, subscription.eventID)
		} else {
			bus.infos[subscription.eventID] = infos
		}
	}
}

func (bus *bus) PublishSync(ctx context.Context, event Event) PublishResult {
	return <-bus.PublishAsync(ctx, event)
}

func (bus *bus) PublishAsync(ctx context.Context, event Event) <-chan PublishResult {
	if event == nil {
		panic("eventbus: nil event")
	}

	infos := bus.copySubscriptions(event.EventID())
	resultCh := make(chan PublishResult, 1)
	if len(infos) == 0 {
		resultCh <- PublishResult{}
		close(resultCh)
		return resultCh
	}

	pubCtx, cancel := bus.publishContext(ctx)
	results := make(chan error, len(infos))

	for _, info := range infos {
		info := info
		go bus.invokeHandler(pubCtx, event, info, results)
	}

	go func() {
		defer close(resultCh)
		if cancel != nil {
			defer cancel()
		}

		result := PublishResult{}
		remaining := len(infos)

		for remaining > 0 {
			select {
			case err := <-results:
				remaining--
				result.Delivered++
				if err != nil {
					result.Errors = append(result.Errors, err)
				}
			case <-pubCtx.Done():
				result.Pending = remaining
				result.Err = pubCtx.Err()
				resultCh <- result
				return
			}
		}

		resultCh <- result
	}()

	return resultCh
}

func (bus *bus) invokeHandler(
	ctx context.Context,
	event Event,
	info *subscriptionInfo,
	results chan<- error,
) {
	var err error
	var panicValue any

	defer func() {
		if recovered := recover(); recovered != nil {
			panicValue = recovered
			err = fmt.Errorf("eventbus: handler panic for %q: %v", event.EventID(), recovered)
		}
		if err != nil {
			bus.handleFailure(HandlerFailure{
				Subscription: Subscription{
					eventID: event.EventID(),
					id:      info.id,
				},
				Event: event,
				Err:   err,
				Panic: panicValue,
			})
		}
		results <- err
	}()

	err = info.cb(ctx, event)
}

func (bus *bus) handleFailure(failure HandlerFailure) {
	if bus.failureHook != nil {
		bus.failureHook(failure)
	}
}

func (bus *bus) publishContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if bus.publishTimeout <= 0 {
		return parent, nil
	}
	return context.WithTimeout(parent, bus.publishTimeout)
}

func (bus *bus) copySubscriptions(eventID EventID) subscriptionInfoList {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	if infos, ok := bus.infos[eventID]; ok {
		cloned := make(subscriptionInfoList, len(infos))
		copy(cloned, infos)
		return cloned
	}
	return subscriptionInfoList{}
}
