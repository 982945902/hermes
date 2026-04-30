package health

import (
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/982945902/hermes/internal/model"
)

type Tier string

const (
	TierExcellent   Tier = "excellent"
	TierUnstable    Tier = "unstable"
	TierUnavailable Tier = "unavailable"
)

const (
	alpha              = 0.30
	targetLatencyMS    = 3500.0
	probeMinInterval   = 30 * time.Second
	probeProbability   = 0.08
	unavailableFailN   = 3
	unavailableSuccess = 0.35
	keyRateCooldown    = 90 * time.Second
	keyAuthCooldown    = 10 * time.Minute
	keyFailureCooldown = 30 * time.Second
	keyFailureN        = 3
)

type Meter struct {
	mu       sync.RWMutex
	stats    map[string]*ModelStats
	keyStats map[string]*KeyStats
	rand     *rand.Rand
	strategy RoutingStrategy
}

type ChannelSnapshot struct {
	ChannelID string       `json:"channel_id"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Models    []ModelStats `json:"models"`
}

type ExternalModelSnapshot struct {
	ExternalModel string       `json:"external_model"`
	Routes        []ModelStats `json:"routes"`
}

type ModelStats struct {
	ChannelID           string    `json:"channel_id"`
	Name                string    `json:"name"`
	Provider            string    `json:"provider"`
	ExternalModel       string    `json:"external_model"`
	UpstreamModel       string    `json:"upstream_model"`
	Requests            int64     `json:"requests"`
	SuccessEWMA         float64   `json:"success_rate"`
	LatencyEWMA         float64   `json:"latency_ms"`
	QualityEWMA         float64   `json:"quality"`
	Score               float64   `json:"score"`
	Tier                Tier      `json:"tier"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastStatusCode      int       `json:"last_status_code"`
	LastError           string    `json:"last_error,omitempty"`
	LastUpdatedAt       time.Time `json:"last_updated_at,omitempty"`
	LastProbeAt         time.Time `json:"last_probe_at,omitempty"`
}

type KeyStatus string

const (
	KeyStatusAvailable   KeyStatus = "available"
	KeyStatusCoolingDown KeyStatus = "cooling_down"
	KeyStatusUnavailable KeyStatus = "unavailable"
)

type KeyStats struct {
	ChannelID           string    `json:"channel_id"`
	KeyID               string    `json:"key_id"`
	Name                string    `json:"name"`
	Requests            int64     `json:"requests"`
	SuccessEWMA         float64   `json:"success_rate"`
	LatencyEWMA         float64   `json:"latency_ms"`
	Score               float64   `json:"score"`
	Status              KeyStatus `json:"status"`
	CooldownUntil       time.Time `json:"cooldown_until,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastStatusCode      int       `json:"last_status_code"`
	LastError           string    `json:"last_error,omitempty"`
	LastUpdatedAt       time.Time `json:"last_updated_at,omitempty"`
}

type Snapshot struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Channels    []ChannelSnapshot       `json:"channels"`
	Models      []ExternalModelSnapshot `json:"models"`
	Keys        []KeyStats              `json:"keys"`
}

func NewMeter(strategyName ...string) *Meter {
	name := ""
	if len(strategyName) > 0 {
		name = strategyName[0]
	}
	return &Meter{
		stats:    map[string]*ModelStats{},
		keyStats: map[string]*KeyStats{},
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
		strategy: NewRoutingStrategy(name),
	}
}

func (m *Meter) Record(channel model.Channel, externalModel string, upstreamModel string, latency time.Duration, statusCode int, success bool, quality float64, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := channel.ID.Hex()
	stat := m.ensureLocked(channel, externalModel, upstreamModel)
	latencyMS := float64(latency.Milliseconds())
	successValue := 0.0
	if success {
		successValue = 1.0
	}
	quality = clamp01(quality)
	if quality == 0 && success {
		quality = 0.75
	}

	if stat.Requests == 0 {
		stat.SuccessEWMA = successValue
		stat.LatencyEWMA = latencyMS
		stat.QualityEWMA = quality
	} else {
		stat.SuccessEWMA = ewma(stat.SuccessEWMA, successValue)
		stat.LatencyEWMA = ewma(stat.LatencyEWMA, latencyMS)
		stat.QualityEWMA = ewma(stat.QualityEWMA, quality)
	}
	stat.ChannelID = id
	stat.Name = channel.Name
	stat.Provider = channel.Provider
	stat.ExternalModel = externalModel
	stat.UpstreamModel = upstreamModel
	stat.Requests++
	stat.LastStatusCode = statusCode
	stat.LastError = errText
	stat.LastUpdatedAt = time.Now()
	if success {
		stat.ConsecutiveFailures = 0
		if stat.Tier == TierUnavailable {
			stat.Tier = TierUnstable
		}
	} else {
		stat.ConsecutiveFailures++
	}
	stat.Score = computeScore(stat)
	stat.Tier = computeTier(stat)
}

func (m *Meter) Snapshot(activeChannels ...[]model.Channel) Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeRoute := map[string]model.ChannelRoute{}
	activeKey := map[string]struct{}{}
	filterRoutes := len(activeChannels) > 0
	if filterRoutes {
		for _, channel := range activeChannels[0] {
			channel.Normalize()
			for _, route := range channel.Routes() {
				key := metricKey(route.Channel.ID.Hex(), route.ExternalModel, route.UpstreamModel)
				activeRoute[key] = route
			}
			for _, key := range channel.ActiveKeys() {
				activeKey[keyMetricKey(channel.ID.Hex(), key.ID)] = struct{}{}
			}
		}
	}

	grouped := map[string]*ChannelSnapshot{}
	byExternalModel := map[string]*ExternalModelSnapshot{}
	seenRoute := map[string]struct{}{}
	for key, stat := range m.stats {
		activeRouteItem, hasActiveFilter := activeRoute[key]
		if filterRoutes && !hasActiveFilter {
			continue
		}
		seenRoute[key] = struct{}{}
		item := *stat
		if hasActiveFilter {
			item.Name = activeRouteItem.Channel.Name
			item.Provider = activeRouteItem.Channel.Provider
		}
		appendSnapshotItem(grouped, byExternalModel, item)
	}
	if filterRoutes {
		for key, route := range activeRoute {
			if _, ok := seenRoute[key]; ok {
				continue
			}
			appendSnapshotItem(grouped, byExternalModel, initialRouteStats(route))
		}
	}

	channels := make([]ChannelSnapshot, 0, len(grouped))
	for _, channel := range grouped {
		sort.Slice(channel.Models, func(i, j int) bool {
			if channel.Models[i].Tier != channel.Models[j].Tier {
				return tierRank(channel.Models[i].Tier) < tierRank(channel.Models[j].Tier)
			}
			if channel.Models[i].Score != channel.Models[j].Score {
				return channel.Models[i].Score > channel.Models[j].Score
			}
			return channel.Models[i].ExternalModel < channel.Models[j].ExternalModel
		})
		channels = append(channels, *channel)
	}
	sort.Slice(channels, func(i, j int) bool {
		leftTier, rightTier := channelTier(channels[i]), channelTier(channels[j])
		if leftTier != rightTier {
			return tierRank(leftTier) < tierRank(rightTier)
		}
		return channelScore(channels[i]) > channelScore(channels[j])
	})

	models := make([]ExternalModelSnapshot, 0, len(byExternalModel))
	for _, externalModel := range byExternalModel {
		sort.Slice(externalModel.Routes, func(i, j int) bool {
			if externalModel.Routes[i].Tier != externalModel.Routes[j].Tier {
				return tierRank(externalModel.Routes[i].Tier) < tierRank(externalModel.Routes[j].Tier)
			}
			if externalModel.Routes[i].Score != externalModel.Routes[j].Score {
				return externalModel.Routes[i].Score > externalModel.Routes[j].Score
			}
			if externalModel.Routes[i].Name != externalModel.Routes[j].Name {
				return externalModel.Routes[i].Name < externalModel.Routes[j].Name
			}
			return externalModel.Routes[i].UpstreamModel < externalModel.Routes[j].UpstreamModel
		})
		models = append(models, *externalModel)
	}
	sort.Slice(models, func(i, j int) bool {
		leftTier, rightTier := externalModelTier(models[i]), externalModelTier(models[j])
		if leftTier != rightTier {
			return tierRank(leftTier) < tierRank(rightTier)
		}
		if externalModelScore(models[i]) != externalModelScore(models[j]) {
			return externalModelScore(models[i]) > externalModelScore(models[j])
		}
		return models[i].ExternalModel < models[j].ExternalModel
	})

	keys := make([]KeyStats, 0, len(m.keyStats))
	for key, stat := range m.keyStats {
		if filterRoutes {
			if _, ok := activeKey[key]; !ok {
				continue
			}
		}
		keys = append(keys, *stat)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Status != keys[j].Status {
			return keyStatusRank(keys[i].Status) < keyStatusRank(keys[j].Status)
		}
		if keys[i].Score != keys[j].Score {
			return keys[i].Score > keys[j].Score
		}
		if keys[i].ChannelID != keys[j].ChannelID {
			return keys[i].ChannelID < keys[j].ChannelID
		}
		return keys[i].KeyID < keys[j].KeyID
	})
	return Snapshot{GeneratedAt: time.Now(), Channels: channels, Models: models, Keys: keys}
}

func (m *Meter) Rank(routes []model.ChannelRoute) []model.ChannelRoute {
	m.mu.Lock()
	defer m.mu.Unlock()

	candidates := make([]RouteCandidate, 0, len(routes))
	now := time.Now()
	for _, route := range routes {
		stat := m.ensureLocked(route.Channel, route.ExternalModel, route.UpstreamModel)
		if stat.Tier == TierUnavailable {
			continue
		}
		keyWeight := m.availableKeyWeightLocked(route.Channel, now)
		if keyWeight <= 0 {
			continue
		}
		candidates = append(candidates, routeCandidate(route, *stat, keyWeight))
	}
	candidates = m.strategy.Rank(RoutingContext{Rand: m.rand, Now: now}, candidates)
	ranked := make([]model.ChannelRoute, len(candidates))
	for i, candidate := range candidates {
		ranked[i] = candidate.Route
	}
	return ranked
}

func (m *Meter) SelectKey(channel model.Channel) (model.ChannelKey, bool) {
	keys := channel.ActiveKeys()
	if len(keys) == 0 {
		return model.ChannelKey{}, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	type candidate struct {
		key   model.ChannelKey
		value float64
	}
	candidates := make([]candidate, 0, len(keys))
	for _, key := range keys {
		stat := m.ensureKeyLocked(channel, key)
		if stat.CooldownUntil.After(now) {
			continue
		}
		score := stat.Score
		if stat.Requests == 0 {
			score = 0.78
		}
		sigma := 0.05
		if stat.Requests == 0 {
			sigma = 0.18
		} else if stat.Status == KeyStatusCoolingDown {
			sigma = 0.12
			score *= 0.75
		}
		weight := float64(key.Weight)
		if weight <= 0 {
			weight = 1
		}
		sampled := score + m.rand.NormFloat64()*sigma
		value := sampled*math.Sqrt(weight) + float64(key.Priority)*0.02
		candidates = append(candidates, candidate{key: key, value: value})
	}
	if len(candidates) == 0 {
		return model.ChannelKey{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].value > candidates[j].value
	})
	return candidates[0].key, true
}

func (m *Meter) RecordKey(channel model.Channel, key model.ChannelKey, latency time.Duration, statusCode int, success bool, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stat := m.ensureKeyLocked(channel, key)
	latencyMS := float64(latency.Milliseconds())
	successValue := 0.0
	if success {
		successValue = 1.0
	}
	if stat.Requests == 0 {
		stat.SuccessEWMA = successValue
		stat.LatencyEWMA = latencyMS
	} else {
		stat.SuccessEWMA = ewma(stat.SuccessEWMA, successValue)
		stat.LatencyEWMA = ewma(stat.LatencyEWMA, latencyMS)
	}
	stat.Requests++
	stat.LastStatusCode = statusCode
	stat.LastError = errText
	stat.LastUpdatedAt = time.Now()
	if success {
		stat.ConsecutiveFailures = 0
		stat.CooldownUntil = time.Time{}
		stat.Status = KeyStatusAvailable
	} else {
		stat.ConsecutiveFailures++
		stat.Status = KeyStatusCoolingDown
		switch {
		case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
			stat.Status = KeyStatusUnavailable
			stat.CooldownUntil = time.Now().Add(keyAuthCooldown)
		case statusCode == http.StatusTooManyRequests:
			stat.CooldownUntil = time.Now().Add(keyRateCooldown)
		case stat.ConsecutiveFailures >= keyFailureN:
			stat.CooldownUntil = time.Now().Add(keyFailureCooldown)
		}
	}
	stat.Score = computeKeyScore(stat)
}

func (m *Meter) ProbeCandidates(routes []model.ChannelRoute, selectedKey string) []model.ChannelRoute {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var result []model.ChannelRoute
	for _, route := range routes {
		if route.Key() == selectedKey {
			continue
		}
		stat := m.ensureLocked(route.Channel, route.ExternalModel, route.UpstreamModel)
		if stat.Tier != TierUnavailable {
			continue
		}
		if now.Sub(stat.LastProbeAt) < probeMinInterval {
			continue
		}
		if m.rand.Float64() > probeProbability {
			continue
		}
		stat.LastProbeAt = now
		result = append(result, route)
	}
	return result
}

func (m *Meter) ensureLocked(channel model.Channel, externalModel string, upstreamModel string) *ModelStats {
	id := metricKey(channel.ID.Hex(), externalModel, upstreamModel)
	if stat, ok := m.stats[id]; ok {
		stat.Name = channel.Name
		stat.Provider = channel.Provider
		stat.ExternalModel = externalModel
		stat.UpstreamModel = upstreamModel
		return stat
	}
	stat := &ModelStats{
		ChannelID:     channel.ID.Hex(),
		Name:          channel.Name,
		Provider:      channel.Provider,
		ExternalModel: externalModel,
		UpstreamModel: upstreamModel,
		SuccessEWMA:   1,
		QualityEWMA:   0.75,
		Score:         0.72,
		Tier:          TierExcellent,
	}
	m.stats[id] = stat
	return stat
}

func metricKey(channelID string, externalModel string, upstreamModel string) string {
	return strings.Join([]string{channelID, externalModel, upstreamModel}, "\x00")
}

func appendSnapshotItem(grouped map[string]*ChannelSnapshot, byExternalModel map[string]*ExternalModelSnapshot, item ModelStats) {
	channel, ok := grouped[item.ChannelID]
	if !ok {
		channel = &ChannelSnapshot{
			ChannelID: item.ChannelID,
			Name:      item.Name,
			Provider:  item.Provider,
			Models:    []ModelStats{},
		}
		grouped[item.ChannelID] = channel
	}
	channel.Models = append(channel.Models, item)

	externalModel, ok := byExternalModel[item.ExternalModel]
	if !ok {
		externalModel = &ExternalModelSnapshot{
			ExternalModel: item.ExternalModel,
			Routes:        []ModelStats{},
		}
		byExternalModel[item.ExternalModel] = externalModel
	}
	externalModel.Routes = append(externalModel.Routes, item)
}

func initialRouteStats(route model.ChannelRoute) ModelStats {
	return ModelStats{
		ChannelID:     route.Channel.ID.Hex(),
		Name:          route.Channel.Name,
		Provider:      route.Channel.Provider,
		ExternalModel: route.ExternalModel,
		UpstreamModel: route.UpstreamModel,
		SuccessEWMA:   1,
		QualityEWMA:   0.75,
		Score:         0.72,
		Tier:          TierExcellent,
	}
}

func (m *Meter) ensureKeyLocked(channel model.Channel, key model.ChannelKey) *KeyStats {
	id := keyMetricKey(channel.ID.Hex(), key.ID)
	if stat, ok := m.keyStats[id]; ok {
		stat.Name = key.Name
		return stat
	}
	stat := &KeyStats{
		ChannelID:   channel.ID.Hex(),
		KeyID:       key.ID,
		Name:        key.Name,
		SuccessEWMA: 1,
		Score:       0.78,
		Status:      KeyStatusAvailable,
	}
	m.keyStats[id] = stat
	return stat
}

func (m *Meter) availableKeyWeightLocked(channel model.Channel, now time.Time) float64 {
	total := 0.0
	for _, key := range channel.ActiveKeys() {
		stat := m.ensureKeyLocked(channel, key)
		if stat.Status == KeyStatusUnavailable || stat.CooldownUntil.After(now) {
			continue
		}
		weight := float64(key.Weight)
		if weight <= 0 {
			weight = 1
		}
		total += weight
	}
	return total
}

func keyMetricKey(channelID string, keyID string) string {
	return strings.Join([]string{channelID, keyID}, "\x00")
}

func channelTier(channel ChannelSnapshot) Tier {
	result := TierUnavailable
	for _, stat := range channel.Models {
		if tierRank(stat.Tier) < tierRank(result) {
			result = stat.Tier
		}
	}
	return result
}

func channelScore(channel ChannelSnapshot) float64 {
	best := 0.0
	for _, stat := range channel.Models {
		if stat.Score > best {
			best = stat.Score
		}
	}
	return best
}

func externalModelTier(model ExternalModelSnapshot) Tier {
	result := TierUnavailable
	for _, stat := range model.Routes {
		if tierRank(stat.Tier) < tierRank(result) {
			result = stat.Tier
		}
	}
	return result
}

func externalModelScore(model ExternalModelSnapshot) float64 {
	best := 0.0
	for _, stat := range model.Routes {
		if stat.Score > best {
			best = stat.Score
		}
	}
	return best
}

func computeScore(stat *ModelStats) float64 {
	latencyScore := 0.72
	if stat.LatencyEWMA > 0 {
		latencyScore = math.Exp(-stat.LatencyEWMA / targetLatencyMS)
	}
	return clamp01(latencyScore*0.35 + stat.SuccessEWMA*0.40 + stat.QualityEWMA*0.25)
}

func computeKeyScore(stat *KeyStats) float64 {
	latencyScore := 0.72
	if stat.LatencyEWMA > 0 {
		latencyScore = math.Exp(-stat.LatencyEWMA / targetLatencyMS)
	}
	return clamp01(latencyScore*0.20 + stat.SuccessEWMA*0.80)
}

func computeTier(stat *ModelStats) Tier {
	if stat.ConsecutiveFailures >= unavailableFailN || stat.SuccessEWMA < unavailableSuccess {
		return TierUnavailable
	}
	if stat.Requests >= 3 && (stat.SuccessEWMA < 0.82 || stat.Score < 0.48 || stat.LatencyEWMA > 12000) {
		return TierUnstable
	}
	return TierExcellent
}

func tierRank(tier Tier) int {
	switch tier {
	case TierExcellent:
		return 0
	case TierUnstable:
		return 1
	default:
		return 2
	}
}

func keyStatusRank(status KeyStatus) int {
	switch status {
	case KeyStatusAvailable:
		return 0
	case KeyStatusCoolingDown:
		return 1
	default:
		return 2
	}
}

func ewma(oldValue float64, newValue float64) float64 {
	return oldValue*(1-alpha) + newValue*alpha
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
