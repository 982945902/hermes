package cache

import (
	"context"
	"log"
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
	byModel   map[string][]model.Channel
	all       []model.Channel
	updatedAt time.Time
}

func NewChannelCache(store *store.Store, interval time.Duration) *ChannelCache {
	return &ChannelCache{
		store:    store,
		interval: interval,
		byID:     map[string]model.Channel{},
		byModel:  map[string][]model.Channel{},
	}
}

func (c *ChannelCache) Load(ctx context.Context) error {
	channels, err := c.store.ListChannels(ctx, true)
	if err != nil {
		return err
	}
	byID := make(map[string]model.Channel, len(channels))
	byModel := make(map[string][]model.Channel)
	all := make([]model.Channel, 0, len(channels))
	for _, channel := range channels {
		channel.Normalize()
		id := channel.ID.Hex()
		byID[id] = channel
		all = append(all, channel)
		if !channel.Enabled {
			continue
		}
		for _, modelName := range channel.Models {
			if modelName == "" {
				continue
			}
			byModel[modelName] = append(byModel[modelName], channel)
		}
	}
	c.mu.Lock()
	c.byID = byID
	c.byModel = byModel
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

func (c *ChannelCache) FindByModel(modelName string) []model.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	channels := c.byModel[modelName]
	out := make([]model.Channel, len(channels))
	copy(out, channels)
	return out
}

func (c *ChannelCache) ListChannels() []model.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]model.Channel, len(c.all))
	copy(out, c.all)
	return out
}

func (c *ChannelCache) ListModels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	models := make([]string, 0, len(c.byModel))
	for modelName := range c.byModel {
		models = append(models, modelName)
	}
	return models
}

func (c *ChannelCache) UpdatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}
