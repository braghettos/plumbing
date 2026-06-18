package discoveryevents_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/krateoplatformops/plumbing/eventbus"
	"github.com/krateoplatformops/plumbing/kubeutil/discoveryevents"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

func TestPublisher_PublishChanges(t *testing.T) {
	var (
		added   []discoveryevents.ResourceAddedEvent
		changed []discoveryevents.ResourceChangedEvent
		removed []discoveryevents.ResourceRemovedEvent
	)

	bus := eventbus.New()

	onAdd := bus.Subscribe(discoveryevents.EventResourceAdded, func(ctx context.Context, event eventbus.Event) error {
		added = append(added, event.(discoveryevents.ResourceAddedEvent))
		return nil
	})

	onChange := bus.Subscribe(discoveryevents.EventResourceChanged, func(ctx context.Context, event eventbus.Event) error {
		changed = append(changed, event.(discoveryevents.ResourceChangedEvent))
		return nil
	})

	onRemove := bus.Subscribe(discoveryevents.EventResourceRemoved, func(ctx context.Context, event eventbus.Event) error {
		removed = append(removed, event.(discoveryevents.ResourceRemovedEvent))
		return nil
	})

	defer func() {
		bus.Unsubscribe(onAdd)
		bus.Unsubscribe(onChange)
		bus.Unsubscribe(onRemove)
	}()

	client := &fakeDiscovery{
		steps: []fakeDiscoveryStep{
			{
				lists: []*metav1.APIResourceList{
					list("apps/v1", resource("deployments", "Deployment", "get", "list", "watch")),
				},
			},
			{
				lists: []*metav1.APIResourceList{
					list("apps/v1", resource("deployments", "Deployment", "get", "list")),
					list("batch/v1", resource("jobs", "Job", "get", "list")),
				},
			},
			{
				lists: []*metav1.APIResourceList{
					list("batch/v1", resource("jobs", "Job", "get", "list")),
				},
			},
		},
	}

	pub := discoveryevents.NewPublisher(client, bus)

	require.NoError(t, pub.PublishChanges(context.Background()))
	require.NoError(t, pub.PublishChanges(context.Background()))
	require.NoError(t, pub.PublishChanges(context.Background()))

	require.Len(t, added, 2)
	require.Equal(t, "apps/v1", added[0].APIVersion)
	require.Equal(t, "Deployment", added[0].Kind)
	require.Equal(t, "deployments", added[0].Name)
	require.Equal(t, []string{"get", "list", "watch"}, added[0].Verbs)
	require.Equal(t, "batch/v1", added[1].APIVersion)
	require.Equal(t, "Job", added[1].Kind)

	require.Len(t, changed, 1)
	require.Equal(t, []string{"get", "list", "watch"}, changed[0].Previous.Verbs)
	require.Equal(t, []string{"get", "list"}, changed[0].Current.Verbs)

	require.Len(t, removed, 1)
	require.Equal(t, "apps/v1", removed[0].APIVersion)
	require.Equal(t, "deployments", removed[0].Name)
}

func ExamplePublisher_subscribe() {
	bus := eventbus.New()

	onAdd := bus.Subscribe(discoveryevents.EventResourceAdded, func(ctx context.Context, event eventbus.Event) error {
		added := event.(discoveryevents.ResourceAddedEvent)
		fmt.Printf("added %s %s\n", added.APIVersion, added.Name)
		return nil
	})

	onChange := bus.Subscribe(discoveryevents.EventResourceChanged, func(ctx context.Context, event eventbus.Event) error {
		changed := event.(discoveryevents.ResourceChangedEvent)
		fmt.Printf("changed %s %v -> %v\n", changed.Current.Name, changed.Previous.Verbs, changed.Current.Verbs)
		return nil
	})

	onRemove := bus.Subscribe(discoveryevents.EventResourceRemoved, func(ctx context.Context, event eventbus.Event) error {
		removed := event.(discoveryevents.ResourceRemovedEvent)
		fmt.Printf("removed %s %s\n", removed.APIVersion, removed.Name)
		return nil
	})

	defer func() {
		bus.Unsubscribe(onAdd)
		bus.Unsubscribe(onChange)
		bus.Unsubscribe(onRemove)
	}()

	client := &fakeDiscovery{
		steps: []fakeDiscoveryStep{
			{
				lists: []*metav1.APIResourceList{
					list("apps/v1", resource("deployments", "Deployment", "get", "list", "watch")),
				},
			},
			{
				lists: []*metav1.APIResourceList{
					list("apps/v1", resource("deployments", "Deployment", "get", "list")),
				},
			},
			{
				lists: nil,
			},
		},
	}

	pub := discoveryevents.NewPublisher(client, bus)

	_ = pub.PublishChanges(context.Background())
	_ = pub.PublishChanges(context.Background())
	_ = pub.PublishChanges(context.Background())

	// Output:
	// added apps/v1 deployments
	// changed deployments [get list watch] -> [get list]
	// removed apps/v1 deployments
}

func TestPublisher_PublishChanges_DiscoveryError(t *testing.T) {
	bus := eventbus.New()
	clientErr := errors.New("discovery unavailable")
	client := &fakeDiscovery{
		steps: []fakeDiscoveryStep{
			{err: clientErr},
		},
	}

	pub := discoveryevents.NewPublisher(client, bus)

	err := pub.PublishChanges(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, clientErr)
	require.ErrorContains(t, err, "discover resources")
}

func TestPublisher_PublishChanges_HandlerError(t *testing.T) {
	var added []discoveryevents.ResourceAddedEvent

	bus := eventbus.New()
	handlerErr := errors.New("subscriber failed")

	failSub := bus.Subscribe(discoveryevents.EventResourceAdded,
		func(ctx context.Context, event eventbus.Event) error {
			return handlerErr
		})
	defer bus.Unsubscribe(failSub)

	client := &fakeDiscovery{
		steps: []fakeDiscoveryStep{
			{
				lists: []*metav1.APIResourceList{
					list("apps/v1", resource("deployments", "Deployment", "get", "list", "watch")),
				},
			},
		},
	}

	pub := discoveryevents.NewPublisher(client, bus)

	err := pub.PublishChanges(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, handlerErr)

	bus.Unsubscribe(failSub)
	okSub := bus.Subscribe(discoveryevents.EventResourceAdded,
		func(ctx context.Context, event eventbus.Event) error {
			added = append(added, event.(discoveryevents.ResourceAddedEvent))
			return nil
		})
	defer bus.Unsubscribe(okSub)

	require.NoError(t, pub.PublishChanges(context.Background()))
	require.Len(t, added, 1)
	require.Equal(t, "deployments", added[0].Name)
}

type fakeDiscovery struct {
	steps []fakeDiscoveryStep
	call  int
}

func (f *fakeDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	if len(f.steps) == 0 {
		return nil, nil
	}

	idx := f.call
	if idx >= len(f.steps) {
		idx = len(f.steps) - 1
	}
	f.call++

	return f.steps[idx].lists, f.steps[idx].err
}

type fakeDiscoveryStep struct {
	lists []*metav1.APIResourceList
	err   error
}

var _ discoveryevents.ResourceDiscovery = (*fakeDiscovery)(nil)
var _ discoveryevents.ResourceDiscovery = (discovery.DiscoveryInterface)(nil)

func list(groupVersion string, resources ...metav1.APIResource) *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: groupVersion,
		APIResources: resources,
	}
}

func resource(name, kind string, verbs ...string) metav1.APIResource {
	return metav1.APIResource{
		Name:  name,
		Kind:  kind,
		Verbs: metav1.Verbs(verbs),
	}
}
