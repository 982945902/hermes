package cache

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/982945902/hermes/internal/model"
	"github.com/982945902/hermes/internal/store"
)

type ChannelCache struct {
	store     *store.Store
	interval  time.Duration
	mu        sync.RWMutex
	byID      map[string]model.Channel
	byModel   map[string][]model.ChannelRoute
	routes    []model.ChannelRoute
	all       []model.Channel
	updatedAt time.Time
}

func NewChannelCache(store *store.Store, interval time.Duration) *ChannelCache {
	return &ChannelCache{
		store:    store,
		interval: interval,
		byID:     map[string]model.Channel{},
		byModel:  map[string][]model.ChannelRoute{},
	}
}

func (c *ChannelCache) Load(ctx context.Context) error {
	channels, err := c.store.ListChannels(ctx, true)
	if err != nil {
		return err
	}
	byID := make(map[string]model.Channel, len(channels))
	byModel := make(map[string][]model.ChannelRoute)
	routes := make([]model.ChannelRoute, 0)
	all := make([]model.Channel, 0, len(channels))
	for _, channel := range channels {
		channel.Normalize()
		id := channel.ID.Hex()
		byID[id] = channel
		all = append(all, channel)
		if !channel.Enabled {
			continue
		}
		for _, route := range channel.Routes() {
			if route.ExternalModel == "" || route.UpstreamModel == "" {
				continue
			}
			byModel[route.ExternalModel] = append(byModel[route.ExternalModel], route)
			routes = append(routes, route)
		}
	}
	c.mu.Lock()
	c.byID = byID
	c.byModel = byModel
	c.routes = routes
	c.all = all
	c.updatedAt = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *ChannelCache) Start(ctx context.Context) {
	if c.interval <= 0 {
		return
	}
	ticker := time.NewTicker(c.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Load(ctx); err != nil {
					log.Printf("reload channel cache: %v", err)
				}
			}
		}
	}()
}

func (c *ChannelCache) ReloadAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Load(ctx); err != nil {
			log.Printf("reload channel cache: %v", err)
		}
	}()
}

func (c *ChannelCache) FindByModel(modelName string) []model.ChannelRoute {
	c.mu.RLock()
	defer c.mu.RUnlock()
	routes := c.byModel[modelName]
	out := make([]model.ChannelRoute, len(routes))
	copy(out, routes)
	return out
}

func (c *ChannelCache) ListChannels() []model.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.Channel, len(c.all))
	copy(out, c.all)
	return out
}

func (c *ChannelCache) ListRoutes() []model.ChannelRoute {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.ChannelRoute, len(c.routes))
	copy(out, c.routes)
	return out
}

func (c *ChannelCache) ListModels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	models := make([]string, 0, len(c.byModel))
	for modelName := range c.byModel {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models
}

func (c *ChannelCache) UpdatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}
