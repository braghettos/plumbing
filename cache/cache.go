package cache

import (
	"container/list"
	"sync"
	"time"
)

type item[V any] struct {
	value  V
	expiry time.Time
	elem   *list.Element
}

func (i item[V]) isExpired() bool {
	return time.Now().After(i.expiry)
}

type ttlCacheOptions struct {
	cleanupInterval time.Duration
	maxEntries      int
}

type Option func(*ttlCacheOptions)

func WithCleanupInterval(interval time.Duration) Option {
	return func(opts *ttlCacheOptions) {
		if interval >= 0 {
			opts.cleanupInterval = interval
		}
	}
}

func WithMaxEntries(maxEntries int) Option {
	return func(opts *ttlCacheOptions) {
		if maxEntries >= 0 {
			opts.maxEntries = maxEntries
		}
	}
}

type TTLCache[K comparable, V any] struct {
	items      map[K]*item[V]
	order      *list.List
	mu         sync.Mutex
	stopCh     chan struct{}
	stopOnce   sync.Once
	maxEntries int
}

func NewTTL[K comparable, V any](opts ...Option) *TTLCache[K, V] {
	cfg := ttlCacheOptions{
		cleanupInterval: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	c := &TTLCache[K, V]{
		items:      make(map[K]*item[V]),
		order:      list.New(),
		stopCh:     make(chan struct{}),
		maxEntries: cfg.maxEntries,
	}

	if cfg.cleanupInterval > 0 {
		ticker := time.NewTicker(cfg.cleanupInterval)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					c.mu.Lock()
					for key, entry := range c.items {
						if entry.isExpired() {
							c.removeEntry(key, entry)
						}
					}
					c.mu.Unlock()
				case <-c.stopCh:
					return
				}
			}
		}()
	}

	return c
}

func (c *TTLCache[K, V]) Set(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if current, found := c.items[key]; found {
		current.value = value
		current.expiry = time.Now().Add(ttl)
		c.order.MoveToFront(current.elem)
		return
	}

	elem := c.order.PushFront(key)
	c.items[key] = &item[V]{
		value:  value,
		expiry: time.Now().Add(ttl),
		elem:   elem,
	}

	if c.maxEntries > 0 && len(c.items) > c.maxEntries {
		c.evictOldest()
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.items[key]
	if !found {
		var zero V
		return zero, false
	}

	if entry.isExpired() {
		val := entry.value
		c.removeEntry(key, entry)
		return val, false
	}

	c.order.MoveToFront(entry.elem)
	return entry.value, true
}

func (c *TTLCache[K, V]) Remove(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, found := c.items[key]; found {
		c.removeEntry(key, entry)
	}
}

func (c *TTLCache[K, V]) Pop(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.items[key]
	if !found {
		var zero V
		return zero, false
	}

	c.removeEntry(key, entry)

	if entry.isExpired() {
		return entry.value, false
	}

	return entry.value, true
}

func (c *TTLCache[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()

	all := make([]K, 0, len(c.items))

	for key, entry := range c.items {
		if entry.isExpired() {
			c.removeEntry(key, entry)
			continue
		}
		all = append(all, key)
	}

	return all
}

func (c *TTLCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.items)
	c.order.Init()
}

func (c *TTLCache[K, V]) Close() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

func (c *TTLCache[K, V]) removeEntry(key K, entry *item[V]) {
	delete(c.items, key)
	c.order.Remove(entry.elem)
}

func (c *TTLCache[K, V]) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	key := elem.Value.(K)
	if entry, found := c.items[key]; found {
		c.removeEntry(key, entry)
	}
}
