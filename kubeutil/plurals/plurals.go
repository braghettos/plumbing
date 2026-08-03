package plurals

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/krateo-platformops/plumbing/cache"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type GetOptions struct {
	Logger       *slog.Logger
	Cache        *cache.TTLCache[string, Info]
	ResolverFunc func(schema.GroupVersionKind) (Info, error)
}

func Get(gvk schema.GroupVersionKind, opts GetOptions) (Info, error) {
	if opts.ResolverFunc == nil {
		opts.ResolverFunc = ResolveAPINames
	}

	var (
		tmp Info
		ok  bool
	)

	if opts.Cache != nil {
		tmp, ok = opts.Cache.Get(gvk.String())
		if ok && opts.Logger != nil {
			opts.Logger.Debug("cache hit", slog.String("gvk", gvk.String()))
		}
	}

	if !ok {
		if opts.Logger != nil {
			opts.Logger.Debug("cache miss", slog.String("gvk", gvk.String()))
		}

		var err error
		tmp, err = opts.ResolverFunc(gvk)
		if err != nil {
			if opts.Logger != nil {
				opts.Logger.Error("unable to discover API names",
					slog.String("gvk", gvk.String()), slog.Any("err", err))
			}
			return Info{}, err
		}

		if opts.Cache != nil {
			opts.Cache.Set(gvk.String(), tmp, time.Hour*48)
		}
	}

	if len(tmp.Plural) == 0 {
		err := &apierrors.StatusError{
			ErrStatus: metav1.Status{
				Status: metav1.StatusFailure,
				Code:   http.StatusNotFound,
				Reason: metav1.StatusReasonNotFound,
				Details: &metav1.StatusDetails{
					Group: gvk.Group,
					Kind:  gvk.Kind,
				},
				Message: fmt.Sprintf("no names found for %q", gvk.GroupVersion().String()),
			}}

		if opts.Logger != nil {
			opts.Logger.Warn(err.ErrStatus.Message)
		}
		return tmp, err
	}

	return tmp, nil
}

func ResolveAPINames(gvk schema.GroupVersionKind) (Info, error) {
	rc, err := rest.InClusterConfig()
	if err != nil {
		return Info{}, err
	}

	dc, err := discovery.NewDiscoveryClientForConfig(rc)
	if err != nil {
		return Info{}, err
	}

	list, err := dc.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return Info{}, err
	}

	if list == nil || len(list.APIResources) == 0 {
		return Info{}, nil
	}

	var tmp Info
	for _, el := range list.APIResources {
		if el.Kind != gvk.Kind {
			continue
		}

		tmp = Info{
			Plural:   el.Name,
			Singular: el.SingularName,
			Shorts:   el.ShortNames,
		}
		break
	}

	return tmp, nil
}

type Info struct {
	Plural   string   `json:"plural"`
	Singular string   `json:"singular"`
	Shorts   []string `json:"shorts"`
}
