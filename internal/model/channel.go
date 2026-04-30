package model

import (
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	ProviderOpenRouter   = "openrouter"
	ProviderDoubaoCoding = "doubao_coding"
	ProviderCustom       = "custom"
)

type Channel struct {
	ID            bson.ObjectID       `bson:"_id,omitempty" json:"id"`
	Name          string              `bson:"name" json:"name"`
	Provider      string              `bson:"provider" json:"provider"`
	BaseURL       string              `bson:"base_url" json:"base_url"`
	APIKey        string              `bson:"api_key" json:"api_key,omitempty"`
	Keys          []ChannelKey        `bson:"keys,omitempty" json:"keys,omitempty"`
	Models        []string            `bson:"models" json:"models"`
	ModelMapping  map[string]string   `bson:"model_mapping" json:"model_mapping"`
	ModelMappings map[string][]string `bson:"model_mappings" json:"model_mappings"`
	ExtraHeaders  map[string]string   `bson:"extra_headers" json:"extra_headers"`
	Strategy      map[string]any      `bson:"strategy" json:"strategy"`
	Enabled       bool                `bson:"enabled" json:"enabled"`
	Priority      int                 `bson:"priority" json:"priority"`
	Weight        int                 `bson:"weight" json:"weight"`
	LastError     string              `bson:"last_error,omitempty" json:"last_error,omitempty"`
	LastTestAt    *time.Time          `bson:"last_test_at,omitempty" json:"last_test_at,omitempty"`
	CreatedAt     time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
}

type ChannelKey struct {
	ID       string `bson:"id" json:"id"`
	Name     string `bson:"name" json:"name"`
	APIKey   string `bson:"api_key" json:"api_key,omitempty"`
	Enabled  *bool  `bson:"enabled,omitempty" json:"enabled,omitempty"`
	Priority int    `bson:"priority" json:"priority"`
	Weight   int    `bson:"weight" json:"weight"`
}

type ChannelRoute struct {
	Channel       Channel
	ExternalModel string
	UpstreamModel string
}

func (r ChannelRoute) Key() string {
	return strings.Join([]string{r.Channel.ID.Hex(), r.ExternalModel, r.UpstreamModel}, "\x00")
}

func (c Channel) Public(maskKey bool) Channel {
	if maskKey {
		c.APIKey = maskAPIKey(c.APIKey)
		keys := make([]ChannelKey, len(c.Keys))
		copy(keys, c.Keys)
		c.Keys = keys
		for i := range c.Keys {
			c.Keys[i].APIKey = maskAPIKey(c.Keys[i].APIKey)
		}
	}
	return c
}

func (c Channel) ActiveKeys() []ChannelKey {
	keys := make([]ChannelKey, 0, len(c.Keys))
	for _, key := range c.Keys {
		key.APIKey = strings.TrimSpace(key.APIKey)
		if key.APIKey == "" || strings.Contains(key.APIKey, "********") || !key.IsEnabled() {
			continue
		}
		if key.Weight <= 0 {
			key.Weight = 1
		}
		keys = append(keys, key)
	}
	return keys
}

func (c Channel) HasConfiguredKey(allowMasked bool) bool {
	for _, key := range c.Keys {
		apiKey := strings.TrimSpace(key.APIKey)
		if !key.IsEnabled() {
			continue
		}
		if apiKey == "" {
			continue
		}
		if allowMasked || !strings.Contains(apiKey, "********") {
			return true
		}
	}
	apiKey := strings.TrimSpace(c.APIKey)
	return apiKey != "" && (allowMasked || !strings.Contains(apiKey, "********"))
}

func (c ChannelKey) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func (c Channel) UpstreamModel(model string) string {
	models := c.UpstreamModels(model)
	if len(models) > 0 {
		return models[0]
	}
	return model
}

func (c Channel) UpstreamModels(model string) []string {
	seen := map[string]struct{}{}
	var result []string
	if c.ModelMappings != nil {
		for _, item := range c.ModelMappings[model] {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	if c.ModelMapping != nil {
		if mapped := strings.TrimSpace(c.ModelMapping[model]); mapped != "" {
			if _, ok := seen[mapped]; !ok {
				result = append(result, mapped)
			}
		}
	}
	if len(result) == 0 {
		model = strings.TrimSpace(model)
		if model != "" {
			result = append(result, model)
		}
	}
	return result
}

func (c Channel) Routes() []ChannelRoute {
	var routes []ChannelRoute
	for _, externalModel := range c.Models {
		externalModel = strings.TrimSpace(externalModel)
		if externalModel == "" {
			continue
		}
		for _, upstreamModel := range c.UpstreamModels(externalModel) {
			routes = append(routes, ChannelRoute{
				Channel:       c,
				ExternalModel: externalModel,
				UpstreamModel: upstreamModel,
			})
		}
	}
	return routes
}

func (c Channel) RoutesForModel(model string) []ChannelRoute {
	if !c.HasModel(model) {
		return nil
	}
	var routes []ChannelRoute
	for _, upstreamModel := range c.UpstreamModels(model) {
		routes = append(routes, ChannelRoute{
			Channel:       c,
			ExternalModel: model,
			UpstreamModel: upstreamModel,
		})
	}
	return routes
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
	c.Keys = normalizeChannelKeys(c.Keys, c.APIKey)
	if len(c.Keys) > 0 {
		c.APIKey = primaryChannelAPIKey(c.Keys)
	}
	if c.ModelMapping == nil {
		c.ModelMapping = map[string]string{}
	}
	if c.ModelMappings == nil {
		c.ModelMappings = map[string][]string{}
	}
	for externalModel, upstreamModel := range c.ModelMapping {
		externalModel = strings.TrimSpace(externalModel)
		upstreamModel = strings.TrimSpace(upstreamModel)
		if externalModel == "" || upstreamModel == "" {
			continue
		}
		if len(c.ModelMappings[externalModel]) == 0 {
			c.ModelMappings[externalModel] = []string{upstreamModel}
		}
	}
	c.Models = normalizeModels(c.Models, c.ModelMappings)
	c.ModelMappings = normalizeModelMappings(c.Models, c.ModelMappings)
	c.ModelMapping = firstModelMappings(c.ModelMappings)
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

func normalizeChannelKeys(keys []ChannelKey, legacyAPIKey string) []ChannelKey {
	legacyAPIKey = strings.TrimSpace(legacyAPIKey)
	legacyIsActual := legacyAPIKey != "" && !strings.Contains(legacyAPIKey, "********")
	if len(keys) == 0 && legacyAPIKey != "" {
		return []ChannelKey{{
			ID:      "default",
			Name:    "Default",
			APIKey:  legacyAPIKey,
			Enabled: boolPtr(true),
			Weight:  1,
		}}
	}

	result := make([]ChannelKey, 0, len(keys))
	seen := map[string]struct{}{}
	for i, key := range keys {
		key.ID = strings.TrimSpace(key.ID)
		key.Name = strings.TrimSpace(key.Name)
		key.APIKey = strings.TrimSpace(key.APIKey)
		if i == 0 && legacyIsActual && (key.APIKey == "" || strings.Contains(key.APIKey, "********")) {
			key.APIKey = legacyAPIKey
		}
		if key.APIKey == "" {
			continue
		}
		if key.ID == "" {
			key.ID = bson.NewObjectID().Hex()
		}
		if _, ok := seen[key.ID]; ok {
			key.ID = bson.NewObjectID().Hex()
		}
		seen[key.ID] = struct{}{}
		if key.Name == "" {
			key.Name = fmt.Sprintf("Key %d", i+1)
		}
		if key.Enabled == nil {
			key.Enabled = boolPtr(true)
		}
		if key.Weight <= 0 {
			key.Weight = 1
		}
		result = append(result, key)
	}
	return result
}

func primaryChannelAPIKey(keys []ChannelKey) string {
	for _, key := range keys {
		if key.IsEnabled() && strings.TrimSpace(key.APIKey) != "" {
			return key.APIKey
		}
	}
	if len(keys) > 0 {
		return keys[0].APIKey
	}
	return ""
}

func normalizeModels(models []string, mappings map[string][]string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(models)+len(mappings))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		result = append(result, modelName)
	}
	for modelName := range mappings {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		result = append(result, modelName)
	}
	return result
}

func normalizeModelMappings(models []string, mappings map[string][]string) map[string][]string {
	result := make(map[string][]string, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		seen := map[string]struct{}{}
		for _, upstreamModel := range mappings[modelName] {
			upstreamModel = strings.TrimSpace(upstreamModel)
			if upstreamModel == "" {
				continue
			}
			if _, ok := seen[upstreamModel]; ok {
				continue
			}
			seen[upstreamModel] = struct{}{}
			result[modelName] = append(result[modelName], upstreamModel)
		}
		if len(result[modelName]) == 0 {
			result[modelName] = []string{modelName}
		}
	}
	return result
}

func firstModelMappings(mappings map[string][]string) map[string]string {
	result := make(map[string]string, len(mappings))
	for modelName, upstreamModels := range mappings {
		if len(upstreamModels) > 0 {
			result[modelName] = upstreamModels[0]
		}
	}
	return result
}

func boolPtr(value bool) *bool {
	return &value
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
