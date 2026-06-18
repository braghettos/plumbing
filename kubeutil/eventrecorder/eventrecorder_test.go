package eventrecorder

import (
	"context"
	"testing"
	"time"

	"github.com/krateoplatformops/plumbing/ptr"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		config       *rest.Config
		recorderName string
		logger       *klog.Logger
		wantErr      bool
	}{
		{
			name:         "nil config should return error",
			ctx:          context.Background(),
			config:       nil,
			recorderName: "test-recorder",
			logger:       nil,
			wantErr:      true,
		},
		{
			name:         "empty recorder name should still work",
			ctx:          context.Background(),
			config:       &rest.Config{Host: "invalid-host"},
			recorderName: "",
			logger:       nil,
			wantErr:      true,
		},
		{
			name:         "correctly configured recorder",
			ctx:          context.Background(),
			config:       &rest.Config{Host: "https://localhost:6443"},
			recorderName: "test-recorder",
			logger:       ptr.To(klog.TODO()),
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder, err := Create(tt.ctx, tt.config, tt.recorderName, tt.logger)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Create() error = nil, wantErr %v", tt.wantErr)
				}
				if recorder != nil {
					t.Errorf("Create() returned recorder when error expected")
				}
			} else {
				if err != nil {
					t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				}
				if recorder == nil {
					t.Errorf("Create() returned nil recorder when success expected")
				}
			}
		})
	}
}

func TestCreateWithThrottle(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		config       *rest.Config
		recorderName string
		logger       *klog.Logger
		wantErr      bool
	}{
		{
			name:         "nil config should return error",
			ctx:          context.Background(),
			config:       nil,
			recorderName: "test-recorder",
			logger:       nil,
			wantErr:      true,
		},
		{
			name:         "empty recorder name should return error",
			ctx:          context.Background(),
			config:       &rest.Config{Host: "invalid-host"},
			recorderName: "",
			logger:       nil,
			wantErr:      true,
		},
		{
			name:         "correctly configured recorder returns throttled wrapper",
			ctx:          context.Background(),
			config:       &rest.Config{Host: "https://localhost:6443"},
			recorderName: "test-recorder",
			logger:       ptr.To(klog.TODO()),
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder, err := CreateWithThrottle(tt.ctx, tt.config, tt.recorderName, tt.logger)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateWithThrottle() error = nil, wantErr %v", tt.wantErr)
				}
				if recorder != nil {
					t.Errorf("CreateWithThrottle() returned recorder when error expected")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateWithThrottle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if recorder == nil {
				t.Errorf("CreateWithThrottle() returned nil recorder when success expected")
			}

			if _, ok := recorder.(*ThrottledRecorder); !ok {
				t.Errorf("CreateWithThrottle() did not return *ThrottledRecorder, got %T", recorder)
			}
		})
	}
}

func TestCreateWithThrottle_UsesConfiguredMaxSilence(t *testing.T) {
	t.Setenv(throttledRecorderMaxSilenceEnvKey, "7s")

	recorder, err := CreateWithThrottle(
		context.Background(),
		&rest.Config{Host: "https://localhost:6443"},
		"test-recorder",
		ptr.To(klog.TODO()),
	)
	if err != nil {
		t.Fatalf("CreateWithThrottle() error = %v", err)
	}

	throttled, ok := recorder.(*ThrottledRecorder)
	if !ok {
		t.Fatalf("CreateWithThrottle() did not return *ThrottledRecorder, got %T", recorder)
	}

	if throttled.maxSilence != 7*time.Second {
		t.Fatalf("expected maxSilence=7s, got %s", throttled.maxSilence)
	}
}
