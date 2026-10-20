package joinids

import (
	"fmt"
	"math"
	"testing"
)

func TestJoinIDs(t *testing.T) {
	cases := []struct {
		in   []int64
		want string
	}{
		{nil, ""},
		{[]int64{7}, "7"},
		{[]int64{1, 22, 333}, "1,22,333"},
		{[]int64{-5, 0, 10}, "-5,0,10"},
		{[]int64{math.MaxInt64, math.MinInt64}, "9223372036854775807,-9223372036854775808"},
	}
	for _, c := range cases {
		if got := JoinIDs(c.in); got != c.want {
			t.Errorf("JoinIDs(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAllocs(t *testing.T) {
	ids := makeIDs(100)
	allocs := testing.AllocsPerRun(100, func() { _ = JoinIDs(ids) })
	if allocs > 1 {
		t.Fatalf("JoinIDs делает %.0f аллокаций на вызов, цель — не больше 1", allocs)
	}
}

func makeIDs(n int) []int64 {
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i) * 7919
	}
	return ids
}

var sink string

func BenchmarkJoinIDs(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		ids := makeIDs(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sink = JoinIDs(ids)
			}
		})
	}
}
