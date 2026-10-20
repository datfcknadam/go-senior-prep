//go:build solution

package slices

func Reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func RemoveAt[T any](s []T, i int) []T {
	_ = s[i] // проверка границ: паника с понятным сообщением
	copy(s[i:], s[i+1:])
	var zero T
	s[len(s)-1] = zero // иначе массив продолжит держать ссылку, и GC её не соберёт
	return s[:len(s)-1]
}

func Chunk[T any](s []T, size int) [][]T {
	if size <= 0 {
		panic("slices: chunk size must be positive")
	}
	chunks := make([][]T, 0, (len(s)+size-1)/size)
	for lo := 0; lo < len(s); lo += size {
		hi := min(lo+size, len(s))
		// cap ограничен hi: append к куску скопирует данные, а не перезапишет соседа.
		chunks = append(chunks, s[lo:hi:hi])
	}
	return chunks
}

func Dedup[T comparable](s []T) []T {
	seen := make(map[T]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
