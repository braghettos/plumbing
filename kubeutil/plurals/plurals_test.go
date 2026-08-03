package plurals_test

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/krateo-platformops/plumbing/cache"
	"github.com/krateo-platformops/plumbing/kubeutil/plurals"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGet(t *testing.T) {
	type fields struct {
		cache   *cache.TTLCache[string, plurals.Info]
		logger  *slog.Logger
		resolve func(gvk schema.GroupVersionKind) (plurals.Info, error)
	}

	tests := []struct {
		name       string
		gvk        schema.GroupVersionKind
		setup      func(*cache.TTLCache[string, plurals.Info])
		resolver   func(gvk schema.GroupVersionKind) (plurals.Info, error)
		want       plurals.Info
		wantErr    bool
		isNotFound bool
	}{
		{
			name: "cache hit",
			gvk:  schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			setup: func(c *cache.TTLCache[string, plurals.Info]) {
				c.Set("apps/v1, Kind=Deployment", plurals.Info{
					Plural:   "deployments",
					Singular: "deployment",
					Shorts:   []string{"deploy"},
				}, time.Minute)
			},
			resolver: func(_ schema.GroupVersionKind) (plurals.Info, error) {
				t.Fatal("should not call resolver")
				return plurals.Info{}, nil
			},
			want: plurals.Info{
				Plural:   "deployments",
				Singular: "deployment",
				Shorts:   []string{"deploy"},
			},
			wantErr: false,
		},

		{
			name: "cache miss, resolved",
			gvk:  schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"},
			setup: func(c *cache.TTLCache[string, plurals.Info]) {
				// empty
			},
			resolver: func(_ schema.GroupVersionKind) (plurals.Info, error) {
				return plurals.Info{
					Plural:   "statefulsets",
					Singular: "statefulset",
					Shorts:   []string{"sts"},
				}, nil
			},
			want: plurals.Info{
				Plural:   "statefulsets",
				Singular: "statefulset",
				Shorts:   []string{"sts"},
			},
			wantErr: false,
		},
		{
			name:  "resolver returns error",
			gvk:   schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"},
			setup: func(c *cache.TTLCache[string, plurals.Info]) {},
			resolver: func(_ schema.GroupVersionKind) (plurals.Info, error) {
				return plurals.Info{}, errors.New("boom")
			},
			want:    plurals.Info{},
			wantErr: true,
		},
		{
			name:  "resolver returns empty plural",
			gvk:   schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			setup: func(c *cache.TTLCache[string, plurals.Info]) {},
			resolver: func(_ schema.GroupVersionKind) (plurals.Info, error) {
				return plurals.Info{}, nil
			},
			want:       plurals.Info{},
			wantErr:    true,
			isNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := cache.NewTTL[string, plurals.Info]()

			tt.setup(cache)

			got, err := plurals.Get(tt.gvk, plurals.GetOptions{
				Logger:       slog.Default(),
				Cache:        cache,
				ResolverFunc: tt.resolver,
			})
			if tt.wantErr {
				assert.Error(t, err)
				if tt.isNotFound {
					assert.True(t, apierrors.IsNotFound(err), "expected IsNotFound=true")
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
