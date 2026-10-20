//go:build !solution

package algos

// IsValidBrackets проверяет, что скобки ()[]{} сбалансированы. Прочие символы игнорируются.
func IsValidBrackets(s string) bool {
	panic("TODO")
}

// MergeIntervals сливает пересекающиеся и соприкасающиеся ([1,3] и [3,5]) интервалы.
// Результат отсортирован по началу. Входной слайс можно менять.
func MergeIntervals(intervals [][2]int) [][2]int {
	panic("TODO")
}

// TopKFrequent возвращает k самых частых элементов. При равной частоте — меньший элемент раньше.
// Результат — по убыванию частоты.
func TopKFrequent(nums []int, k int) []int {
	panic("TODO")
}

// GroupAnagrams группирует слова-анаграммы (слова из строчных латинских букв). Порядок групп — по первому появлению,
// порядок слов внутри группы — как во входе.
func GroupAnagrams(words []string) [][]string {
	panic("TODO")
}

// LongestUniqueSubstring возвращает длину (в рунах) самой длинной подстроки без повторяющихся рун.
func LongestUniqueSubstring(s string) int {
	panic("TODO")
}

// MinWindowSum возвращает минимальную длину непрерывного подмассива с суммой >= target,
// или 0, если такого нет. Все числа положительные.
func MinWindowSum(nums []int, target int) int {
	panic("TODO")
}
