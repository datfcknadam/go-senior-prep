//go:build !solution

package joinids

import "strconv"

// JoinIDs склеивает id через запятую: [1 2 3] → "1,2,3".
// Работает правильно, но медленно. Оптимизируй.
func JoinIDs(ids []int64) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += strconv.Itoa(int(id))
	}
	return s
}
