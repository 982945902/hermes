package health

import (
	"net/http"
	"testing"
	"time"

	"github.com/982945902/hermes/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSelectKeySkipsAuthCoolingKey(t *testing.T) {
	enabled := true
	channel := model.Channel{
		ID:   bson.NewObjectID(),
		Name: "test",
		Keys: []model.ChannelKey{
			{ID: "bad", Name: "Bad", APIKey: "sk-bad", Enabled: &enabled, Weight: 1},
			{ID: "good", Name: "Good", APIKey: "sk-good", Enabled: &enabled, Weight: 1},
		},
	}
	channel.Normalize()
	meter := NewMeter()

	meter.RecordKey(channel, channel.Keys[0], 10*time.Millisecond, http.StatusUnauthorized, false, "unauthorized")
	selected, ok := meter.SelectKey(channel)
	if !ok {
		t.Fatalf("expected a selectable key")
	}
	if selected.ID != "good" {
		t.Fatalf("expected healthy key to be selected, got %q", selected.ID)
	}
}

func TestDefaultRoutingStrategyIsPowerOfTwoChoices(t *testing.T) {
	meter := NewMeter()
	if meter.strategy.Name() != StrategyPowerOfTwo {
		t.Fatalf("expected default strategy %q, got %q", StrategyPowerOfTwo, meter.strategy.Name())
	}
}

func TestRouteCandidateCapacityIncreasesUtility(t *testing.T) {
	channel := model.Channel{ID: bson.NewObjectID(), Name: "test", Weight: 1}
	channel.Normalize()
	route := model.ChannelRoute{Channel: channel, ExternalModel: "deepseek-v3", UpstreamModel: "deepseek-v3"}
	stats := ModelStats{Score: 0.82, Requests: 10, Tier: TierExcellent}

	singleKey := routeCandidate(route, stats, 1)
	multiKey := routeCandidate(route, stats, 4)
	if multiKey.CapacityWeight != 4 {
		t.Fatalf("expected capacity weight 4, got %f", multiKey.CapacityWeight)
	}
	if multiKey.Utility <= singleKey.Utility {
		t.Fatalf("expected more valid keys to increase routing utility: single=%f multi=%f", singleKey.Utility, multiKey.Utility)
	}
}

func TestRankSkipsChannelWithoutAvailableKeys(t *testing.T) {
	enabled := true
	badChannel := model.Channel{
		ID:     bson.NewObjectID(),
		Name:   "bad-channel",
		Models: []string{"deepseek-v3"},
		Keys: []model.ChannelKey{
			{ID: "bad", Name: "Bad", APIKey: "sk-bad", Enabled: &enabled, Weight: 1},
		},
	}
	goodChannel := model.Channel{
		ID:     bson.NewObjectID(),
		Name:   "good-channel",
		Models: []string{"deepseek-v3"},
		Keys: []model.ChannelKey{
			{ID: "good", Name: "Good", APIKey: "sk-good", Enabled: &enabled, Weight: 1},
		},
	}
	badChannel.Normalize()
	goodChannel.Normalize()
	meter := NewMeter()
	meter.RecordKey(badChannel, badChannel.Keys[0], 10*time.Millisecond, http.StatusUnauthorized, false, "unauthorized")

	ranked := meter.Rank([]model.ChannelRoute{
		{Channel: badChannel, ExternalModel: "deepseek-v3", UpstreamModel: "deepseek-v3"},
		{Channel: goodChannel, ExternalModel: "deepseek-v3", UpstreamModel: "deepseek-v3"},
	})
	if len(ranked) != 1 {
		t.Fatalf("expected only one ranked route, got %d", len(ranked))
	}
	if ranked[0].Channel.ID != goodChannel.ID {
		t.Fatalf("expected good channel to remain ranked, got %q", ranked[0].Channel.Name)
	}
}

func TestSnapshotIncludesActiveRoutesWithoutSamples(t *testing.T) {
	channel := model.Channel{
		ID:     bson.NewObjectID(),
		Name:   "cold-channel",
		Models: []string{"deepseek-v3"},
		ModelMappings: map[string][]string{
			"deepseek-v3": {"deepseek-v3-a", "deepseek-v3-b"},
		},
	}
	channel.Normalize()
	meter := NewMeter()

	snapshot := meter.Snapshot([]model.Channel{channel})
	if len(snapshot.Models) != 1 {
		t.Fatalf("expected one external model group, got %d", len(snapshot.Models))
	}
	if snapshot.Models[0].ExternalModel != "deepseek-v3" {
		t.Fatalf("expected deepseek-v3 group, got %q", snapshot.Models[0].ExternalModel)
	}
	if len(snapshot.Models[0].Routes) != 2 {
		t.Fatalf("expected two cold routes, got %d", len(snapshot.Models[0].Routes))
	}
	for _, route := range snapshot.Models[0].Routes {
		if route.Requests != 0 {
			t.Fatalf("expected cold route to have zero requests")
		}
		if route.Score == 0 || route.Tier != TierExcellent {
			t.Fatalf("expected initialized score and tier, got score=%f tier=%s", route.Score, route.Tier)
		}
	}
}
