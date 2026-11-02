package mc

import (
	"slices"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestGeyserEnv(t *testing.T) {
	cases := []struct {
		join string
		want []string
	}{
		{"", []string{"GEYSER_BEDROCK_PORT=19132"}},
		{"play.example.com", []string{"GEYSER_BEDROCK_PORT=19132", "GEYSER_BEDROCK_SIGNALING_ADVERTISE_ADDRESSES=play.example.com"}},
		{"play.example.com:25565", []string{"GEYSER_BEDROCK_PORT=19132", "GEYSER_BEDROCK_SIGNALING_ADVERTISE_ADDRESSES=play.example.com"}},
		{"203.0.113.5:25570", []string{"GEYSER_BEDROCK_PORT=19132", "GEYSER_BEDROCK_SIGNALING_ADVERTISE_ADDRESSES=203.0.113.5"}},
		{"[2001:db8::1]:25565", []string{"GEYSER_BEDROCK_PORT=19132", "GEYSER_BEDROCK_SIGNALING_ADVERTISE_ADDRESSES=[2001:db8::1]"}},
		{"2001:db8::1", []string{"GEYSER_BEDROCK_PORT=19132", "GEYSER_BEDROCK_SIGNALING_ADVERTISE_ADDRESSES=[2001:db8::1]"}},
	}
	for _, c := range cases {
		got := geyserEnv(store.Server{BedrockPort: 19132, JoinAddress: c.join})
		if !slices.Equal(got, c.want) {
			t.Errorf("join %q: got %q, want %q", c.join, got, c.want)
		}
	}
	if got := geyserEnv(store.Server{JoinAddress: "play.example.com"}); got != nil {
		t.Errorf("no Bedrock port: got %q, want nothing", got)
	}
}
