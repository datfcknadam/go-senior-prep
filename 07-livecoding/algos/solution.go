//go:build solution

package algos

import (
	"cmp"
	"container/heap"
	"slices"
)

func IsValidBrackets(s string) bool {
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	var stack []rune
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			stack = append(stack, r)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[r] {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

func MergeIntervals(intervals [][2]int) [][2]int {
	if len(intervals) == 0 {
		return nil
	}
	slices.SortFunc(intervals, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	out := [][2]int{intervals[0]}
	for _, cur := range intervals[1:] {
		last := &out[len(out)-1]
		if cur[0] <= last[1] {
			last[1] = max(last[1], cur[1])
		} else {
			out = append(out, cur)
		}
	}
	return out
}

type freq struct{ num, count int }

// minHeap держит k лучших; на вершине — худший из них.
type minHeap []freq

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	if h[i].count != h[j].count {
		return h[i].count < h[j].count
	}
	return h[i].num > h[j].num
}
func (h minHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)   { *h = append(*h, x.(freq)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func TopKFrequent(nums []int, k int) []int {
	counts := make(map[int]int)
	for _, n := range nums {
		counts[n]++
	}
	h := &minHeap{}
	for num, c := range counts {
		heap.Push(h, freq{num, c})
		if h.Len() > k {
			heap.Pop(h)
		}
	}
	out := make([]int, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(freq).num
	}
	return out
}

func GroupAnagrams(words []string) [][]string {
	index := make(map[[26]byte]int) // ключ — частоты букв; массив сравним и годится как ключ мапы
	var groups [][]string
	for _, w := range words {
		var key [26]byte
		for _, r := range w {
			if r >= 'a' && r <= 'z' {
				key[r-'a']++
			}
		}
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], w)
	}
	return groups
}

func LongestUniqueSubstring(s string) int {
	runes := []rune(s)
	last := make(map[rune]int)
	best, left := 0, 0
	for right, r := range runes {
		if i, ok := last[r]; ok && i >= left {
			left = i + 1
		}
		last[r] = right
		best = max(best, right-left+1)
	}
	return best
}

func MinWindowSum(nums []int, target int) int {
	best, sum, left := 0, 0, 0
	for right, n := range nums {
		sum += n
		for sum >= target {
			if w := right - left + 1; best == 0 || w < best {
				best = w
			}
			sum -= nums[left]
			left++
		}
	}
	return best
}
