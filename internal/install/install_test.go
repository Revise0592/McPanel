package install

import (
	"slices"
	"testing"
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
