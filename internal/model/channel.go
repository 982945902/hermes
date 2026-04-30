package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	ProviderOpenRouter   = "openrouter"
	ProviderDoubaoCoding = "doubao_coding"
	ProviderCustom       = "custom"
)

type Channel struct {
	ID           bson.ObjectID     `bson:"_id,omitempty" json:"id"`
	Name         string            `bson:"name" json:"name"`
	Provider     string            `bson:"provider" json:"provider"`
	BaseURL      string            `bson:"base_url" json:"base_url"`
	APIKey       string            `bson:"api_key" json:"api_key,omitempty"`
	Models       []string          `bson:"models" json:"models"`
	ModelMapping map[string]string `bson:"model_mapping" json:"model_mapping"`
	ExtraHeaders map[string]string `bson:"extra_headers" json:"extra_headers"`
	Strategy     map[string]any    `bson:"strategy" json:"strategy"`
	Enabled      bool              `bson:"enabled" json:"enabled"`
	Priority     int               `bson:"priority" json:"priority"`
	Weight       int               `bson:"weight" json:"weight"`
	LastError    string            `bson:"last_error,omitempty" json:"last_error,omitempty"`
	LastTestAt   *time.Time        `bson:"last_test_at,omitempty" json:"last_test_at,omitempty"`
	CreatedAt    time.Time         `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time         `bson:"updated_at" json:"updated_at"`
}

func (c Channel) Public(maskKey bool) Channel {
	if maskKey {
		c.APIKey = maskAPIKey(c.APIKey)
	}
	return c
}

func (c Channel) UpstreamModel(model string) string {
	if c.ModelMapping != nil {
		if mapped := c.ModelMapping[model]; mapped != "" {
			return mapped
		}
	}
	return model
}

func (c Channel) HasModel(model string) bool {
	for _, item := range c.Models {
		if item == model {
			return true
		}
	}
	return false
}

func (c *Channel) Normalize() {
	if c.Provider == "" {
		c.Provider = ProviderCustom
	}
	if c.Models == nil {
		c.Models = []string{}
	}
	if c.ModelMapping == nil {
		c.ModelMapping = map[string]string{}
	}
	if c.ExtraHeaders == nil {
		c.ExtraHeaders = map[string]string{}
	}
	if c.Strategy == nil {
		c.Strategy = map[string]any{}
	}
	if c.Weight <= 0 {
		c.Weight = 1
	}
}

func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "********" + key[len(key)-4:]
}
