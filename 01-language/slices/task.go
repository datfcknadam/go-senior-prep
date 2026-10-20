//go:build !solution

package slices

// Reverse разворачивает слайс на месте.
func Reverse[T any](s []T) {
	panic("TODO")
}

// RemoveAt удаляет элемент с индексом i с сохранением порядка
// и возвращает укороченный слайс. Паникует, если i вне диапазона.
func RemoveAt[T any](s []T, i int) []T {
	panic("TODO")
}

// Chunk разбивает s на куски длиной size (последний может быть короче).
// Паникует, если size <= 0.
func Chunk[T any](s []T, size int) [][]T {
	panic("TODO")
}

// Dedup возвращает новый слайс без дубликатов в порядке первых вхождений.
func Dedup[T comparable](s []T) []T {
	panic("TODO")
}
