package algos

import (
	"reflect"
	"testing"
)

func TestIsValidBrackets(t *testing.T) {
	cases := map[string]bool{
		"":           true,
		"()":         true,
		"([]{})":     true,
		"a(b[c]d)e":  true,
		"(]":         false,
		"([)]":       false,
		"((":         false,
		"))":         false,
		"{[()()]}[]": true,
		"func() { }": true,
		"}{":         false,
	}
	for in, want := range cases {
		if got := IsValidBrackets(in); got != want {
			t.Errorf("IsValidBrackets(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMergeIntervals(t *testing.T) {
	cases := []struct{ in, want [][2]int }{
		{nil, nil},
		{[][2]int{{1, 3}}, [][2]int{{1, 3}}},
		{[][2]int{{8, 10}, {1, 3}, {2, 6}, {15, 18}}, [][2]int{{1, 6}, {8, 10}, {15, 18}}},
		{[][2]int{{1, 4}, {4, 5}}, [][2]int{{1, 5}}},
		{[][2]int{{1, 10}, {2, 3}, {4, 5}}, [][2]int{{1, 10}}},
	}
	for _, c := range cases {
		if got := MergeIntervals(c.in); !reflect.DeepEqual(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("MergeIntervals(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTopKFrequent(t *testing.T) {
	cases := []struct {
		nums []int
		k    int
		want []int
	}{
		{[]int{1, 1, 1, 2, 2, 3}, 2, []int{1, 2}},
		{[]int{4, 4, 5, 5, 6}, 2, []int{4, 5}},
		{[]int{7}, 1, []int{7}},
		{[]int{3, 1, 2}, 2, []int{1, 2}},
	}
	for _, c := range cases {
		if got := TopKFrequent(c.nums, c.k); !reflect.DeepEqual(got, c.want) {
			t.Errorf("TopKFrequent(%v, %d) = %v, want %v", c.nums, c.k, got, c.want)
		}
	}
}

func TestGroupAnagrams(t *testing.T) {
	got := GroupAnagrams([]string{"eat", "tea", "tan", "ate", "nat", "bat"})
	want := [][]string{{"eat", "tea", "ate"}, {"tan", "nat"}, {"bat"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := GroupAnagrams(nil); len(got) != 0 {
		t.Errorf("GroupAnagrams(nil) = %v", got)
	}
}

func TestLongestUniqueSubstring(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"abcabcbb": 3,
		"bbbbb":    1,
		"pwwkew":   3,
		"abba":     2,
		"привет":   6,
		"мама":     2,
	}
	for in, want := range cases {
		if got := LongestUniqueSubstring(in); got != want {
			t.Errorf("LongestUniqueSubstring(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestMinWindowSum(t *testing.T) {
	cases := []struct {
		nums   []int
		target int
		want   int
	}{
		{[]int{2, 3, 1, 2, 4, 3}, 7, 2},
		{[]int{1, 4, 4}, 4, 1},
		{[]int{1, 1, 1, 1}, 10, 0},
		{nil, 1, 0},
		{[]int{1, 2, 3, 4, 5}, 15, 5},
	}
	for _, c := range cases {
		if got := MinWindowSum(c.nums, c.target); got != c.want {
			t.Errorf("MinWindowSum(%v, %d) = %d, want %d", c.nums, c.target, got, c.want)
		}
	}
}
