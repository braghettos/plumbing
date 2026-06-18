package wait

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func ExampleUntil_success() {
	value, err := Until(context.Background(), nil, func(ctx context.Context) (string, error) {
		return "ready", nil
	})

	fmt.Println(value, err == nil)
	// Output:
	// ready true
}

func ExampleUntil_contextCanceled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Until(ctx, nil, func(context.Context) (string, error) {
		return "", errors.New("not ready")
	})

	fmt.Println(err == context.Canceled)
	// Output:
	// true
}

func ExampleUntilWithOptions() {
	value, err := UntilWithOptions(
		context.Background(),
		func(ctx context.Context) (int, error) {
			return 42, nil
		},
		Options{
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     time.Second,
		},
	)

	fmt.Println(value, err == nil)
	// Output:
	// 42 true
}
