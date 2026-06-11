package batchkoi

import (
	"reflect"
	"testing"
)

func TestComputeRetention(t *testing.T) {
	cases := []struct {
		name      string
		revisions []int32
		keepCount int
		keepRev   []int
		wantDereg []int32
		wantKept  []int32
	}{
		{"keep all when count 0", []int32{5, 4, 3}, 0, nil, nil, []int32{5, 4, 3}},
		{"keep newest 2", []int32{5, 4, 3, 2}, 2, nil, []int32{3, 2}, []int32{5, 4}},
		{"keep count plus protected rev", []int32{5, 4, 3, 2}, 2, []int{2}, []int32{3}, []int32{5, 4, 2}},
		{"keep count exceeds total", []int32{3, 2}, 5, nil, nil, []int32{3, 2}},
		{"protect outside count", []int32{5, 4, 3, 2, 1}, 1, []int{3}, []int32{4, 2, 1}, []int32{5, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dereg, kept := computeRetention(tc.revisions, tc.keepCount, tc.keepRev)
			if !reflect.DeepEqual(dereg, tc.wantDereg) {
				t.Errorf("deregister: got %v, want %v", dereg, tc.wantDereg)
			}
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept: got %v, want %v", kept, tc.wantKept)
			}
		})
	}
}
