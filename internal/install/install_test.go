package install

import (
	"slices"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestSortVersionsDesc(t *testing.T) {
	vs := []string{"1.20.4", "26.1", "1.21", "1.21.11", "1.9", "26.1.2", "1.21.2"}
	SortVersionsDesc(vs)
	want := []string{"26.1.2", "26.1", "1.21.11", "1.21.2", "1.21", "1.20.4", "1.9"}
	if !slices.Equal(vs, want) {
		t.Errorf("got %v, want %v", vs, want)
	}
}

func TestBedrockZipVersion(t *testing.T) {
	if v := BedrockZipVersion("bedrock-server-1.26.52.3.zip"); v != "1.26.52.3" {
		t.Errorf("got %q", v)
	}
	if v := BedrockZipVersion("server.zip"); v != "unknown" {
		t.Errorf("got %q", v)
	}
}

func TestIsNewerBuild(t *testing.T) {
	tests := []struct {
		sv     store.Server
		latest string
		want   bool
	}{
		{store.Server{Type: store.TypePaper, Build: "120"}, "121", true},
		{store.Server{Type: store.TypePaper, Build: "121"}, "121", false},
		{store.Server{Type: store.TypePaper, Build: ""}, "121", false},
		{store.Server{Type: store.TypeFabric, Build: "0.16.9"}, "0.16.10", true},
		{store.Server{Type: store.TypeBedrock, Version: "1.21.50.07"}, "1.21.51.02", true},
		{store.Server{Type: store.TypeBedrock, Version: "unknown"}, "1.21.51.02", false},
		{store.Server{Type: store.TypeVanilla, Version: "1.21.4"}, "", false},
	}
	for _, tt := range tests {
		if got := IsNewerBuild(tt.sv, tt.latest); got != tt.want {
			t.Errorf("IsNewerBuild(%+v, %q) = %v, want %v", tt.sv, tt.latest, got, tt.want)
		}
	}
}
