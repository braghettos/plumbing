package eventrecorder

import (
	"context"
	"fmt"

	"github.com/krateoplatformops/plumbing/env"
	"github.com/krateoplatformops/plumbing/ptr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	record "k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
)

const throttledRecorderMaxSilenceEnvKey = "EVENTRECORDER_MAX_SILENCE"

func Create(ctx context.Context, rc *rest.Config, recorderName string, logger *klog.Logger) (record.EventRecorder, error) {
	if rc == nil {
		return nil, fmt.Errorf("rest.Config cannot be nil")
	}

	if recorderName == "" {
		return nil, fmt.Errorf("recorderName cannot be empty")
	}

	clientset, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	// Use the global Kubernetes scheme that includes all standard types
	eventScheme := scheme.Scheme

	eventBroadcaster := record.NewBroadcaster(&record.EventSinkImpl{
		Interface: clientset.EventsV1(),
	})

	eventBroadcaster.StartLogging(ptr.Deref(logger, klog.TODO().V(4)))
	err = eventBroadcaster.StartRecordingToSinkWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return eventBroadcaster.NewRecorder(eventScheme, recorderName), nil
}

// CreateWithThrottle creates a standard Kubernetes EventRecorder and wraps it
// with a state-aware throttling recorder.
//
// Unchanged events are suppressed, but one event is forcibly published after a
// continuous suppression interval to avoid indefinite silence.
//
// The suppression interval is configured through EVENTRECORDER_MAX_SILENCE
// (Go duration format, e.g. "2m", "30s"). If unset or invalid, the default is used.
func CreateWithThrottle(ctx context.Context, rc *rest.Config, recorderName string, logger *klog.Logger) (record.EventRecorder, error) {
	recorder, err := Create(ctx, rc, recorderName, logger)
	if err != nil {
		return nil, err
	}

	maxSilence := env.Duration(throttledRecorderMaxSilenceEnvKey, defaultMaxSilence)
	return NewStateAwareRecorderWithMaxSilence(recorder, maxSilence), nil
}
