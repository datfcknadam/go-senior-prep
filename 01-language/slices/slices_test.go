package slices

import (
	"reflect"
	"testing"
)

func TestReverse(t *testing.T) {
	cases := [][]int{nil, {1}, {1, 2}, {1, 2, 3, 4, 5}}
	want := [][]int{nil, {1}, {2, 1}, {5, 4, 3, 2, 1}}
	for i, c := range cases {
		Reverse(c)
		if !reflect.DeepEqual(c, want[i]) {
			t.Errorf("Reverse: got %v, want %v", c, want[i])
		}
	}

	s := []int{1, 2, 3}
	if allocs := testing.AllocsPerRun(100, func() { Reverse(s) }); allocs != 0 {
		t.Errorf("Reverse allocs = %v, want 0", allocs)
	}
}

func TestRemoveAt(t *testing.T) {
	s := []int{1, 2, 3, 4}
	got := RemoveAt(s, 1)
	if !reflect.DeepEqual(got, []int{1, 3, 4}) {
		t.Fatalf("got %v", got)
	}
	if &got[0] != &s[0] {
		t.Error("RemoveAt не должен аллоцировать новый массив")
	}

	p1, p2 := new(int), new(int)
	ps := []*int{p1, p2}
	ps = RemoveAt(ps, 0)
	if ps[:2][1] != nil {
		t.Error("освободившийся элемент нужно обнулить, иначе утечка для GC")
	}

	defer func() {
		if recover() == nil {
			t.Error("ожидалась паника для индекса вне диапазона")
		}
	}()
	RemoveAt([]int{1}, 5)
}

func TestChunk(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	got := Chunk(s, 2)
	want := [][]int{{1, 2}, {3, 4}, {5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	_ = append(got[0], 100)
	if got[1][0] != 3 {
		t.Error("append к первому куску перезаписал второй: ограничь cap")
	}

	if len(Chunk([]int(nil), 3)) != 0 {
		t.Error("Chunk(nil) должен вернуть пустой результат")
	}

	defer func() {
		if recover() == nil {
			t.Error("ожидалась паника при size <= 0")
		}
	}()
	Chunk(s, 0)
}

func TestDedup(t *testing.T) {
	s := []string{"b", "a", "b", "c", "a"}
	got := Dedup(s)
	if !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Fatalf("got %v", got)
	}
	if !reflect.DeepEqual(s, []string{"b", "a", "b", "c", "a"}) {
		t.Error("исходный слайс изменён")
	}
}
