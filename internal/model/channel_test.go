package model

import "testing"

func TestChannelNormalizeBuildsDefaultKeyFromLegacyAPIKey(t *testing.T) {
	channel := Channel{
		Name:    "test",
		APIKey:  "sk-test",
		Models:  []string{"external"},
		Enabled: true,
	}

	channel.Normalize()

	if channel.APIKey != "sk-test" {
		t.Fatalf("expected legacy api key to remain primary, got %q", channel.APIKey)
	}
	if len(channel.Keys) != 1 {
		t.Fatalf("expected one default key, got %d", len(channel.Keys))
	}
	if channel.Keys[0].ID != "default" || channel.Keys[0].APIKey != "sk-test" || !channel.Keys[0].IsEnabled() {
		t.Fatalf("unexpected default key: %#v", channel.Keys[0])
	}
}

func TestChannelPublicMasksKeyPool(t *testing.T) {
	channel := Channel{
		APIKey: "sk-1234567890",
		Keys: []ChannelKey{
			{ID: "primary", Name: "Primary", APIKey: "sk-1234567890"},
		},
	}
	channel.Normalize()

	public := channel.Public(true)

	if public.APIKey == channel.APIKey {
		t.Fatalf("expected legacy api key to be masked")
	}
	if public.Keys[0].APIKey == channel.Keys[0].APIKey {
		t.Fatalf("expected pooled api key to be masked")
	}
}
