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
