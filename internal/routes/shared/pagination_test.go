// SPDX-License-Identifier: AGPL-3.0-only

package shared

import "testing"

func TestPaginationResolve(t *testing.T) {
	p := Pagination{Base: "page", DefaultCount: 30, MaxCount: 20000}

	cases := []struct {
		name       string
		start      OptInt
		count      OptInt
		wantOffset int
		wantLimit  int
	}{
		{"first page", OptInt{Value: 1, Set: true}, OptInt{Value: 60, Set: true}, 0, 60},
		{"second page", OptInt{Value: 2, Set: true}, OptInt{Value: 60, Set: true}, 60, 60},
		{"zero start is the first page", OptInt{Value: 0, Set: true}, OptInt{Value: 45, Set: true}, 0, 45},
		{"deep page", OptInt{Value: 101, Set: true}, OptInt{Value: 60, Set: true}, 6000, 60},
		{"count zero means no limit", OptInt{Value: 1, Set: true}, OptInt{Value: 0, Set: true}, 0, 20000},
		{"count absent uses the default", OptInt{}, OptInt{}, 0, 30},
		{"count above the maximum is clamped", OptInt{Value: 1, Set: true}, OptInt{Value: 50000, Set: true}, 0, 20000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offset, limit := p.Resolve(tc.start, tc.count)
			if offset != tc.wantOffset || limit != tc.wantLimit {
				t.Fatalf("got offset=%d limit=%d, want offset=%d limit=%d", offset, limit, tc.wantOffset, tc.wantLimit)
			}
		})
	}
}

func TestPaginationOffsetBase(t *testing.T) {
	p := Pagination{Base: "offset", DefaultCount: 30, MaxCount: 20000}
	offset, limit := p.Resolve(OptInt{Value: 120, Set: true}, OptInt{Value: 60, Set: true})
	if offset != 120 || limit != 60 {
		t.Fatalf("got offset=%d limit=%d, want 120/60", offset, limit)
	}
}
