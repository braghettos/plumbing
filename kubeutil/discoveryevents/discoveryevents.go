package discoveryevents

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/krateoplatformops/plumbing/eventbus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	EventResourceAdded   eventbus.EventID = "k8s.discovery.resource.added"
	EventResourceRemoved eventbus.EventID = "k8s.discovery.resource.removed"
	EventResourceChanged eventbus.EventID = "k8s.discovery.resource.changed"
)

type ResourceDiscovery interface {
	ServerPreferredResources() ([]*metav1.APIResourceList, error)
}

type ResourceSnapshot struct {
	Kind       string
	APIVersion string
	Name       string
	Verbs      []string
}

type ResourceAddedEvent struct {
	ResourceSnapshot
}

func (e ResourceAddedEvent) EventID() eventbus.EventID {
	return EventResourceAdded
}

type ResourceRemovedEvent struct {
	ResourceSnapshot
}

func (e ResourceRemovedEvent) EventID() eventbus.EventID {
	return EventResourceRemoved
}

type ResourceChangedEvent struct {
	Previous ResourceSnapshot
	Current  ResourceSnapshot
}

func (e ResourceChangedEvent) EventID() eventbus.EventID {
	return EventResourceChanged
}

type Publisher struct {
	client ResourceDiscovery
	bus    eventbus.Bus
	seen   map[string]ResourceSnapshot
}

func NewPublisher(client ResourceDiscovery, bus eventbus.Bus) *Publisher {
	return &Publisher{
		client: client,
		bus:    bus,
		seen:   make(map[string]ResourceSnapshot),
	}
}

func (p *Publisher) PublishNewResources(ctx context.Context) error {
	return p.PublishChanges(ctx)
}

func (p *Publisher) PublishChanges(ctx context.Context) error {
	lists, err := p.client.ServerPreferredResources()
	if err != nil {
		return fmt.Errorf("discover resources: %w", err)
	}

	current := make(map[string]ResourceSnapshot)

	for _, list := range lists {
		for _, resource := range list.APIResources {
			if !isPublishable(resource) {
				continue
			}

			key := resourceKey(list.GroupVersion, resource)
			snapshot := newSnapshot(list.GroupVersion, resource)
			current[key] = snapshot

			previous, ok := p.seen[key]
			if !ok {
				if err := p.publish(ctx, ResourceAddedEvent{ResourceSnapshot: snapshot}); err != nil {
					return err
				}
				continue
			}

			if !snapshot.Equal(previous) {
				if err := p.publish(ctx, ResourceChangedEvent{
					Previous: previous,
					Current:  snapshot,
				}); err != nil {
					return err
				}
			}
		}
	}

	removedKeys := make([]string, 0, len(p.seen))
	for key := range p.seen {
		if _, ok := current[key]; !ok {
			removedKeys = append(removedKeys, key)
		}
	}
	sort.Strings(removedKeys)

	for _, key := range removedKeys {
		if err := p.publish(ctx, ResourceRemovedEvent{ResourceSnapshot: p.seen[key]}); err != nil {
			return err
		}
	}

	p.seen = current

	return nil
}

func (p *Publisher) publish(ctx context.Context, event eventbus.Event) error {
	result := p.bus.PublishSync(ctx, event)
	if result.Err != nil {
		return result.Err
	}
	if len(result.Errors) > 0 {
		return result.Errors[0]
	}
	return nil
}

func newSnapshot(groupVersion string, resource metav1.APIResource) ResourceSnapshot {
	return ResourceSnapshot{
		Kind:       resource.Kind,
		APIVersion: groupVersion,
		Name:       resource.Name,
		Verbs:      append([]string(nil), resource.Verbs...),
	}
}

func (r ResourceSnapshot) Equal(other ResourceSnapshot) bool {
	return r.Kind == other.Kind &&
		r.APIVersion == other.APIVersion &&
		r.Name == other.Name &&
		slices.Equal(r.Verbs, other.Verbs)
}

func resourceKey(groupVersion string, resource metav1.APIResource) string {
	return groupVersion + "/" + resource.Name
}

func isPublishable(resource metav1.APIResource) bool {
	if resource.Name == "" || resource.Kind == "" {
		return false
	}

	if resource.Group == "" && resource.Version == "" && len(resource.Verbs) == 0 {
		// not required, but filters obviously incomplete entries
	}

	return true
}
