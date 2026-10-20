//go:build solution

package joinids

import (
	"strconv"
	"strings"
)

func JoinIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	// Точный размер результата — Grow сделает ровно одну аллокацию.
	size := len(ids) - 1
	for _, id := range ids {
		size += digits(id)
	}
	var b strings.Builder
	b.Grow(size)

	var buf [20]byte // хватает на любой int64 со знаком; живёт на стеке
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(strconv.AppendInt(buf[:0], id, 10))
	}
	return b.String() // без копирования: Builder отдаёт свой буфер
}

func digits(n int64) int {
	d := 1
	if n < 0 {
		d++          // минус
		if n == -n { // math.MinInt64
			return 20
		}
		n = -n
	}
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}
