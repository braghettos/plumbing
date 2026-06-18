package eventbus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailureHookReceivesHandlerError(t *testing.T) {
	handlerErr := errors.New("handler failed")
	var got HandlerFailure

	bus := New(WithFailureHook(func(failure HandlerFailure) {
		got = failure
	}))

	sub := bus.Subscribe(testEvent{}.EventID(), func(ctx context.Context, event Event) error {
		return handlerErr
	})
	defer bus.Unsubscribe(sub)

	result := bus.PublishSync(context.Background(), testEvent{Name: "alpha"})

	require.Error(t, result.Errors[0])
	require.ErrorIs(t, result.Errors[0], handlerErr)
	require.ErrorIs(t, got.Err, handlerErr)
	require.Equal(t, sub, got.Subscription)
	require.Equal(t, testEvent{Name: "alpha"}, got.Event)
	require.Nil(t, got.Panic)
}

func TestSlogFailureHookLogsErrorAndPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	bus := New(WithFailureHook(SlogFailureHook(logger)))

	errSub := bus.Subscribe(testEvent{}.EventID(), func(ctx context.Context, event Event) error {
		return errors.New("boom")
	})
	panicSub := bus.Subscribe(testEvent{}.EventID(), func(ctx context.Context, event Event) error {
		panic("kaboom")
	})
	defer bus.Unsubscribe(errSub)
	defer bus.Unsubscribe(panicSub)

	result := bus.PublishSync(context.Background(), testEvent{Name: "beta"})

	require.Len(t, result.Errors, 2)
	output := buf.String()
	require.Contains(t, output, `"msg":"event handler failed"`)
	require.Contains(t, output, `"event_id":"test.event"`)
	require.Contains(t, output, `"subscription_id":0`)
	require.Contains(t, output, `"subscription_id":1`)
	require.Contains(t, output, `"err":"boom"`)
	require.Contains(t, output, `"panic":"kaboom"`)
	require.Contains(t, output, `"name":"beta"`)
}

type testEvent struct {
	Name string `json:"name"`
}

func (testEvent) EventID() EventID {
	return "test.event"
}
