package health

import (
	"math"
	"math/rand"
	"sort"
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
	stats map[string]*ChannelStats
	rand  *rand.Rand
}

type ChannelStats struct {
	ChannelID           string    `json:"channel_id"`
	Name                string    `json:"name"`
	Provider            string    `json:"provider"`
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
	GeneratedAt time.Time      `json:"generated_at"`
	Channels    []ChannelStats `json:"channels"`
}

func NewMeter() *Meter {
	return &Meter{
		stats: map[string]*ChannelStats{},
		rand:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (m *Meter) Record(channel model.Channel, latency time.Duration, statusCode int, success bool, quality float64, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := channel.ID.Hex()
	stat := m.ensureLocked(channel)
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

func (m *Meter) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channels := make([]ChannelStats, 0, len(m.stats))
	for _, stat := range m.stats {
		channels = append(channels, *stat)
	}
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Tier != channels[j].Tier {
			return tierRank(channels[i].Tier) < tierRank(channels[j].Tier)
		}
		return channels[i].Score > channels[j].Score
	})
	return Snapshot{GeneratedAt: time.Now(), Channels: channels}
}

func (m *Meter) Rank(channels []model.Channel) []model.Channel {
	m.mu.Lock()
	defer m.mu.Unlock()

	type candidate struct {
		channel model.Channel
		value   float64
	}
	candidates := make([]candidate, 0, len(channels))
	for _, channel := range channels {
		stat := m.ensureLocked(channel)
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
		weight := float64(channel.Weight)
		if weight <= 0 {
			weight = 1
		}
		sampled := score + m.rand.NormFloat64()*sigma
		value := sampled*math.Sqrt(weight) + float64(channel.Priority)*0.02
		candidates = append(candidates, candidate{channel: channel, value: value})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].value > candidates[j].value
	})
	ranked := make([]model.Channel, len(candidates))
	for i, candidate := range candidates {
		ranked[i] = candidate.channel
	}
	return ranked
}

func (m *Meter) ProbeCandidates(channels []model.Channel, selectedID string) []model.Channel {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var result []model.Channel
	for _, channel := range channels {
		if channel.ID.Hex() == selectedID {
			continue
		}
		stat := m.ensureLocked(channel)
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
		result = append(result, channel)
	}
	return result
}

func (m *Meter) ensureLocked(channel model.Channel) *ChannelStats {
	id := channel.ID.Hex()
	if stat, ok := m.stats[id]; ok {
		stat.Name = channel.Name
		stat.Provider = channel.Provider
		return stat
	}
	stat := &ChannelStats{
		ChannelID:   id,
		Name:        channel.Name,
		Provider:    channel.Provider,
		SuccessEWMA: 1,
		QualityEWMA: 0.75,
		Score:       0.72,
		Tier:        TierExcellent,
	}
	m.stats[id] = stat
	return stat
}

func computeScore(stat *ChannelStats) float64 {
	latencyScore := 0.72
	if stat.LatencyEWMA > 0 {
		latencyScore = math.Exp(-stat.LatencyEWMA / targetLatencyMS)
	}
	return clamp01(latencyScore*0.35 + stat.SuccessEWMA*0.40 + stat.QualityEWMA*0.25)
}

func computeTier(stat *ChannelStats) Tier {
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
