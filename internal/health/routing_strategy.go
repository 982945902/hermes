package health

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/model"
)

const (
	StrategyPowerOfTwo RoutingStrategyName = "p2c"
	StrategyWeighted   RoutingStrategyName = "weighted"
	StrategySoftmax    RoutingStrategyName = "softmax"
	StrategyBucket     RoutingStrategyName = "bucket"
	StrategyBandit     RoutingStrategyName = "bandit"
)

type RoutingStrategyName string

type RoutingStrategy interface {
	Name() RoutingStrategyName
	Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate
}

type RoutingContext struct {
	Rand *rand.Rand
	Now  time.Time
}

type RouteCandidate struct {
	Route          model.ChannelRoute
	Stats          ModelStats
	Score          float64
	Weight         float64
	CapacityWeight float64
	Utility        float64
	Requests       int64
	Tier           Tier
}

func NewRoutingStrategy(name string) RoutingStrategy {
	switch normalizeStrategyName(name) {
	case StrategyWeighted:
		return weightedRandomStrategy{}
	case StrategySoftmax:
		return softmaxStrategy{temperature: 0.12}
	case StrategyBucket:
		return bucketStrategy{}
	case StrategyBandit:
		return banditStrategy{exploration: 0.22}
	default:
		return powerOfTwoStrategy{}
	}
}

func normalizeStrategyName(name string) RoutingStrategyName {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "-")
	switch name {
	case "", "p2c", "power-of-two", "power-of-two-choices", "power-of-2":
		return StrategyPowerOfTwo
	case "weighted", "weighted-random", "weighted-rand":
		return StrategyWeighted
	case "softmax", "temperature":
		return StrategySoftmax
	case "bucket", "histogram", "tiered":
		return StrategyBucket
	case "bandit", "ucb", "multi-armed-bandit":
		return StrategyBandit
	default:
		return StrategyPowerOfTwo
	}
}

func routeCandidate(route model.ChannelRoute, stats ModelStats, keyWeight float64) RouteCandidate {
	score := stats.Score
	if stats.Requests == 0 {
		score = 0.62
	}
	if stats.Tier == TierUnstable {
		score *= 0.72
	}
	score = clamp01(score)

	channelWeight := float64(route.Channel.Weight)
	if channelWeight <= 0 {
		channelWeight = 1
	}
	if keyWeight <= 0 {
		keyWeight = 1
	}
	weight := channelWeight * keyWeight
	utility := score*math.Sqrt(weight) + float64(route.Channel.Priority)*0.02
	return RouteCandidate{
		Route:          route,
		Stats:          stats,
		Score:          score,
		Weight:         weight,
		CapacityWeight: keyWeight,
		Utility:        utility,
		Requests:       stats.Requests,
		Tier:           stats.Tier,
	}
}

type powerOfTwoStrategy struct{}

func (powerOfTwoStrategy) Name() RoutingStrategyName {
	return StrategyPowerOfTwo
}

func (powerOfTwoStrategy) Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate {
	out := make([]RouteCandidate, 0, len(candidates))
	pool := cloneCandidates(candidates)
	for len(pool) > 0 {
		if len(pool) == 1 {
			out = append(out, pool[0])
			break
		}
		leftIdx := weightedIndex(ctx.Rand, pool, func(item RouteCandidate) float64 {
			return positive(item.Weight)
		})
		left := pool[leftIdx]
		pool = removeCandidate(pool, leftIdx)
		rightIdx := weightedIndex(ctx.Rand, pool, func(item RouteCandidate) float64 {
			return positive(item.Weight)
		})
		right := pool[rightIdx]
		pool = removeCandidate(pool, rightIdx)
		if noisyUtility(ctx.Rand, right) > noisyUtility(ctx.Rand, left) {
			left, right = right, left
		}
		out = append(out, left)
		pool = append(pool, right)
	}
	return out
}

type weightedRandomStrategy struct{}

func (weightedRandomStrategy) Name() RoutingStrategyName {
	return StrategyWeighted
}

func (weightedRandomStrategy) Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate {
	return weightedShuffle(ctx.Rand, candidates, func(item RouteCandidate) float64 {
		return positive(item.Weight) * math.Pow(math.Max(item.Score, 0.05), 4)
	})
}

type softmaxStrategy struct {
	temperature float64
}

func (softmaxStrategy) Name() RoutingStrategyName {
	return StrategySoftmax
}

func (s softmaxStrategy) Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate {
	if len(candidates) == 0 {
		return nil
	}
	temperature := s.temperature
	if temperature <= 0 {
		temperature = 0.12
	}
	maxUtility := candidates[0].Utility
	for _, candidate := range candidates[1:] {
		if candidate.Utility > maxUtility {
			maxUtility = candidate.Utility
		}
	}
	return weightedShuffle(ctx.Rand, candidates, func(item RouteCandidate) float64 {
		return positive(item.Weight) * math.Exp((item.Utility-maxUtility)/temperature)
	})
}

type bucketStrategy struct{}

func (bucketStrategy) Name() RoutingStrategyName {
	return StrategyBucket
}

func (bucketStrategy) Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate {
	out := make([]RouteCandidate, 0, len(candidates))
	pools := [][]RouteCandidate{
		filterCandidates(candidates, func(item RouteCandidate) bool { return item.Score >= 0.90 }),
		filterCandidates(candidates, func(item RouteCandidate) bool { return item.Score >= 0.80 && item.Score < 0.90 }),
		filterCandidates(candidates, func(item RouteCandidate) bool { return item.Score < 0.80 }),
	}
	bucketWeights := []float64{0.70, 0.25, 0.05}
	for len(out) < len(candidates) {
		availableBuckets := make([]RouteCandidate, 0, len(pools))
		for i, pool := range pools {
			if len(pool) > 0 {
				availableBuckets = append(availableBuckets, RouteCandidate{Weight: bucketWeights[i], Score: float64(i)})
			}
		}
		if len(availableBuckets) == 0 {
			break
		}
		bucketPick := weightedIndex(ctx.Rand, availableBuckets, func(item RouteCandidate) float64 {
			return item.Weight
		})
		bucket := int(availableBuckets[bucketPick].Score)
		itemIdx := weightedIndex(ctx.Rand, pools[bucket], func(item RouteCandidate) float64 {
			return positive(item.Weight) * math.Pow(math.Max(item.Score, 0.05), 3)
		})
		out = append(out, pools[bucket][itemIdx])
		pools[bucket] = removeCandidate(pools[bucket], itemIdx)
	}
	return out
}

type banditStrategy struct {
	exploration float64
}

func (banditStrategy) Name() RoutingStrategyName {
	return StrategyBandit
}

func (s banditStrategy) Rank(ctx RoutingContext, candidates []RouteCandidate) []RouteCandidate {
	out := cloneCandidates(candidates)
	total := int64(0)
	for _, item := range out {
		total += item.Requests
	}
	if total < 1 {
		total = 1
	}
	exploration := s.exploration
	if exploration <= 0 {
		exploration = 0.22
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := banditValue(out[i], total, exploration)
		right := banditValue(out[j], total, exploration)
		if left != right {
			return left > right
		}
		return out[i].Route.Key() < out[j].Route.Key()
	})
	return out
}

func banditValue(item RouteCandidate, total int64, exploration float64) float64 {
	bonus := exploration * math.Sqrt(math.Log(float64(total+1))/float64(item.Requests+1))
	capacityBonus := math.Log1p(item.CapacityWeight) * 0.04
	return item.Score + bonus + capacityBonus + float64(item.Route.Channel.Priority)*0.02
}

func noisyUtility(r *rand.Rand, item RouteCandidate) float64 {
	if r == nil {
		return item.Utility
	}
	sigma := 0.035
	if item.Requests == 0 {
		sigma = 0.18
	} else if item.Tier == TierUnstable {
		sigma = 0.08
	}
	return item.Utility + r.NormFloat64()*sigma
}

func weightedShuffle(r *rand.Rand, candidates []RouteCandidate, weight func(RouteCandidate) float64) []RouteCandidate {
	out := make([]RouteCandidate, 0, len(candidates))
	pool := cloneCandidates(candidates)
	for len(pool) > 0 {
		idx := weightedIndex(r, pool, weight)
		out = append(out, pool[idx])
		pool = removeCandidate(pool, idx)
	}
	return out
}

func weightedIndex(r *rand.Rand, candidates []RouteCandidate, weight func(RouteCandidate) float64) int {
	if len(candidates) <= 1 {
		return 0
	}
	total := 0.0
	for _, candidate := range candidates {
		total += positive(weight(candidate))
	}
	if total <= 0 {
		return int(randFloat(r) * float64(len(candidates)))
	}
	pick := randFloat(r) * total
	for i, candidate := range candidates {
		pick -= positive(weight(candidate))
		if pick <= 0 {
			return i
		}
	}
	return len(candidates) - 1
}

func randFloat(r *rand.Rand) float64 {
	if r == nil {
		return rand.Float64()
	}
	return r.Float64()
}

func removeCandidate(candidates []RouteCandidate, idx int) []RouteCandidate {
	copy(candidates[idx:], candidates[idx+1:])
	return candidates[:len(candidates)-1]
}

func cloneCandidates(candidates []RouteCandidate) []RouteCandidate {
	out := make([]RouteCandidate, len(candidates))
	copy(out, candidates)
	return out
}

func filterCandidates(candidates []RouteCandidate, keep func(RouteCandidate) bool) []RouteCandidate {
	out := make([]RouteCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if keep(candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

func positive(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0.0001
	}
	return value
}
