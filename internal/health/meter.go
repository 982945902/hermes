package health

import (
	"math"
	"math/rand"
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
)

type Meter struct {
	mu    sync.RWMutex
	stats map[string]*ModelStats
	rand  *rand.Rand
}

type ChannelSnapshot struct {
	ChannelID string       `json:"channel_id"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Models    []ModelStats `json:"models"`
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

type Snapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Channels    []ChannelSnapshot `json:"channels"`
}

func NewMeter() *Meter {
	return &Meter{
		stats: map[string]*ModelStats{},
		rand:  rand.New(rand.NewSource(time.Now().UnixNano())),
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

	activeRoute := map[string]model.Channel{}
	filterRoutes := len(activeChannels) > 0
	if filterRoutes {
		for _, channel := range activeChannels[0] {
			channel.Normalize()
			for _, route := range channel.Routes() {
				key := metricKey(route.Channel.ID.Hex(), route.ExternalModel, route.UpstreamModel)
				activeRoute[key] = route.Channel
			}
		}
	}

	grouped := map[string]*ChannelSnapshot{}
	for key, stat := range m.stats {
		activeChannel, hasActiveFilter := activeRoute[key]
		if filterRoutes && !hasActiveFilter {
			continue
		}
		item := *stat
		if hasActiveFilter {
			item.Name = activeChannel.Name
			item.Provider = activeChannel.Provider
		}
		channel, ok := grouped[stat.ChannelID]
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
	return Snapshot{GeneratedAt: time.Now(), Channels: channels}
}

func (m *Meter) Rank(routes []model.ChannelRoute) []model.ChannelRoute {
	m.mu.Lock()
	defer m.mu.Unlock()

	type candidate struct {
		route model.ChannelRoute
		value float64
	}
	candidates := make([]candidate, 0, len(routes))
	for _, route := range routes {
		stat := m.ensureLocked(route.Channel, route.ExternalModel, route.UpstreamModel)
		if stat.Tier == TierUnavailable {
			continue
		}
		score := stat.Score
		if stat.Requests == 0 {
			score = 0.62
		}
		sigma := 0.08
		if stat.Requests == 0 {
			sigma = 0.22
		} else if stat.Tier == TierUnstable {
			sigma = 0.16
			score *= 0.72
		}
		weight := float64(route.Channel.Weight)
		if weight <= 0 {
			weight = 1
		}
		sampled := score + m.rand.NormFloat64()*sigma
		value := sampled*math.Sqrt(weight) + float64(route.Channel.Priority)*0.02
		candidates = append(candidates, candidate{route: route, value: value})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].value > candidates[j].value
	})
	ranked := make([]model.ChannelRoute, len(candidates))
	for i, candidate := range candidates {
		ranked[i] = candidate.route
	}
	return ranked
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

func computeScore(stat *ModelStats) float64 {
	latencyScore := 0.72
	if stat.LatencyEWMA > 0 {
		latencyScore = math.Exp(-stat.LatencyEWMA / targetLatencyMS)
	}
	return clamp01(latencyScore*0.35 + stat.SuccessEWMA*0.40 + stat.QualityEWMA*0.25)
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
