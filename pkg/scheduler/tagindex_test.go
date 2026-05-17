package scheduler

import (
	"sort"
	"testing"
)

func TestTagIndex_Candidates(t *testing.T) {
	ti := newTagIndex()
	ti.add("w1", []string{"gpu", "cuda-12.0"})
	ti.add("w2", []string{"cpu", "avx2"})
	ti.add("w3", []string{"gpu", "cuda-11.0"})

	tests := []struct {
		name     string
		required []string
		want     []string
	}{
		{"single tag", []string{"gpu"}, []string{"w1", "w3"}},
		{"intersection of two tags", []string{"gpu", "cuda-12.0"}, []string{"w1"}},
		{"distinct tag", []string{"cpu"}, []string{"w2"}},
		{"unknown tag", []string{"tpu"}, nil},
		{"empty required", nil, nil},
		{"partial mismatch", []string{"gpu", "avx2"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ti.candidates(tt.required)
			sort.Strings(got)
			if !sameTags(got, tt.want) {
				t.Errorf("candidates(%v) = %v, want %v", tt.required, got, tt.want)
			}
		})
	}
}

func TestTagIndex_Remove(t *testing.T) {
	ti := newTagIndex()
	ti.add("w1", []string{"gpu"})
	ti.add("w2", []string{"gpu"})

	ti.remove("w1", []string{"gpu"})
	if got := ti.candidates([]string{"gpu"}); len(got) != 1 || got[0] != "w2" {
		t.Errorf("after removing w1, candidates = %v, want [w2]", got)
	}

	// Removing the last worker of a tag drops the empty tag entry.
	ti.remove("w2", []string{"gpu"})
	if _, ok := ti.index["gpu"]; ok {
		t.Error("empty tag entry should be deleted from the index")
	}
}

func TestTagIndex_Update(t *testing.T) {
	ti := newTagIndex()
	ti.add("w1", []string{"gpu", "cuda-11.0"})

	// A no-op update (identical tags) must preserve membership.
	ti.update("w1", []string{"gpu", "cuda-11.0"}, []string{"gpu", "cuda-11.0"})
	if got := ti.candidates([]string{"cuda-11.0"}); len(got) != 1 {
		t.Errorf("no-op update changed membership: %v", got)
	}

	// A real update moves the worker from the old tag set to the new one.
	ti.update("w1", []string{"gpu", "cuda-11.0"}, []string{"gpu", "cuda-12.0"})
	if got := ti.candidates([]string{"cuda-11.0"}); len(got) != 0 {
		t.Errorf("stale tag still indexed: %v", got)
	}
	if got := ti.candidates([]string{"cuda-12.0"}); len(got) != 1 {
		t.Errorf("new tag not indexed: %v", got)
	}
}

func TestSameTags(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"same set, different order", []string{"a", "b"}, []string{"b", "a"}, true},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"different content", []string{"a"}, []string{"b"}, false},
		{"duplicate mismatch", []string{"a", "a"}, []string{"a", "b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameTags(tt.a, tt.b); got != tt.want {
				t.Errorf("sameTags(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
