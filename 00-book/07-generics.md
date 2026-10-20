# Глава 7. Дженерики и итераторы

## Зачем

Дженерики (1.18) и итераторы (1.23) — самые заметные изменения языка за последние годы. На собеседовании спрашивают,
как они реализованы, почему бывают медленнее ручного кода и когда их не стоит применять.

---

## 1. Функции с параметрами типа 🟢

```go
func Map[T, U any](s []T, f func(T) U) []U {
    out := make([]U, 0, len(s))
    for _, v := range s {
        out = append(out, f(v))
    }
    return out
}

func Max[T cmp.Ordered](a, b T) T {
    if a > b {
        return a
    }
    return b
}

ids := Map(users, func(u User) int64 { return u.ID }) // типы выведены
m := Max[float64](1, 2.5)                             // или указаны явно
```

## 2. Ограничения 🟢

Ограничение — это **интерфейс**, задающий множество допустимых типов:

```go
type Number interface {
    ~int | ~int32 | ~int64 | ~float64 // ~ = «любой тип с таким underlying»
}

func Sum[T Number](xs []T) T {
    var total T
    for _, x := range xs {
        total += x
    }
    return total
}

type Money int64
Sum([]Money{100, 250}) // работает благодаря ~int64
```

- Встроенные ограничения: `any`, `comparable` (можно `==`, годится в ключ мапы), `cmp.Ordered` (`<`, `>`).
- Смешанное ограничение: `interface{ ~[]byte | ~string }`, или с методами: `interface{ ~int; String() string }`.
- Интерфейс с union (`|`) или `~` можно использовать **только как ограничение**, не как тип переменной.

## 3. Дженерик-типы 🟢

```go
type Stack[T any] struct {
    items []T
}

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

func (s *Stack[T]) Pop() (T, bool) {
    var zero T
    if len(s.items) == 0 {
        return zero, false
    }
    v := s.items[len(s.items)-1]
    s.items[len(s.items)-1] = zero // не держать ссылку
    s.items = s.items[:len(s.items)-1]
    return v, true
}

type Cache[K comparable, V any] struct {
    mu sync.Mutex
    m  map[K]V
}
```

- Нулевое значение параметра типа: `var zero T` (или `*new(T)`).
- Стандартная библиотека: `slices`, `maps`, `cmp`, `sync.OnceValue`, `atomic.Pointer[T]`, `iter`.

## 4. Ограничения языка 🟡

- У **методов** не бывает собственных параметров типа: `func (s *Stack[T]) Map[U any]()` нельзя. Пиши свободную функцию.
- Нет специализации: нельзя написать «отдельную реализацию для `int`». Type switch по `T` напрямую невозможен, только через `any(v).(type)`.
- Нет ковариантности: `[]Dog` не является `[]Animal`, и `Stack[*Dog]` не является `Stack[Animal]`.
- Вывод типов работает по аргументам функции, а с 1.21 ещё по типу присваивания и для функций-аргументов. По результату он не работает: `Zero[int]()` пишут явно.
- С Go 1.26 параметр типа может ссылаться на сам дженерик-тип в своём ограничении: `type Ordered[T Ordered[T]] interface{ Less(T) bool }`.

## 5. Когда дженерики нужны, а когда нет 🟡

**Нужны:**
- контейнеры и структуры данных: кэш, очередь, множество, LRU;
- утилиты над слайсами, мапами и каналами (`Map`, `Filter`, `Merge`);
- типобезопасные обёртки вместо `any`: `atomic.Pointer[Config]` вместо `atomic.Value`.

**Не нужны:**
- если достаточно поведения через обычный интерфейс (`io.Reader`, `fmt.Stringer`). `func Write[W io.Writer](w W)` ничем не лучше `func Write(w io.Writer)`;
- если реализация одна и тип один. Дженерик ради «может пригодиться» усложняет код.

Правило из официального блога: сначала пиши код с конкретными типами; дженерик вводи, когда пишешь тот же код в третий раз.

## 6. Итераторы (Go 1.23) 🟡

Итератор — функция, которая вызывает `yield` для каждого элемента:

```go
// пакет iter
type Seq[V any] func(yield func(V) bool)
type Seq2[K, V any] func(yield func(K, V) bool)
```

```go
func (s *Stack[T]) All() iter.Seq[T] {
    return func(yield func(T) bool) {
        for i := len(s.items) - 1; i >= 0; i-- {
            if !yield(s.items[i]) { // false = потребитель сделал break: ОБЯЗАТЕЛЬНО остановиться
                return
            }
        }
    }
}

for v := range stack.All() {
    if v == target {
        break // компилятор превратит break в return false из yield
    }
}
```

- Итераторы в stdlib: `maps.Keys`, `maps.Values`, `maps.All`, `slices.All`, `slices.Values`, `slices.Backward`, `slices.Chunk`,
  `strings.Lines`, `strings.SplitSeq`, `bytes.Lines`. Сборщики: `slices.Collect`, `slices.Sorted`, `maps.Collect`.
- Итераторы ленивы: `slices.Sorted(maps.Keys(m))` не создаёт промежуточный слайс ключей до сортировки.
- **Pull-итератор** `next, stop := iter.Pull(seq)` превращает push-итератор в «дай следующий» (например, чтобы обойти два итератора параллельно).
  Обязательно вызывай `stop()`, иначе утечёт горутина-подобная корутина.
- Когда применять: обход своей структуры данных без копирования в слайс, ленивое чтение строк или страниц из БД, конвейеры фильтров.
- Если вызвать `yield` после того, как он вернул `false`, будет panic в рантайме.

## Под капотом 🔴

- Полная мономорфизация (C++, Rust) раздувает бинарник и время компиляции. Полный боксинг (Java) медленный.
  Go выбрал середину: **GC shape stenciling + dictionaries**.
- **Форма (shape)** определяется underlying type, при этом все указатели — одна форма. `Max[int]` и `Max[int64]` получат разный код,
  а `Max[*User]` и `Max[*Order]` — один экземпляр.
- Каждому экземпляру передаётся скрытый аргумент — **словарь**: дескрипторы реальных типов, itab'ы для методов ограничения.
- **Следствие:** вызов метода ограничения у указательного типа идёт через словарь, то есть косвенно, как через интерфейс, и часто без инлайна.
  `Sum[int]` работает так же быстро, как ручной код, а `Process[T Stringer]` с указателями не быстрее, чем `Process(fmt.Stringer)`.
- `range` по функции компилятор переписывает: тело цикла становится замыканием `yield`, а `break`/`return` внутри превращаются в флаги и `return false`.
  После инлайна итератора накладные расходы обычно исчезают.

## Ловушки

- Итератор игнорирует `false` от `yield` — panic при `break`.
- `iter.Pull` без `stop()`.
- Дженерик-интерфейс там, где хватило бы обычного.
- Ожидание, что `Stack[*Dog]` подойдёт туда, где ждут `Stack[Animal]`.

## Спросят

1. Как реализованы дженерики в Go? Почему они бывают медленнее ручного кода?
2. Что значит `~` в ограничении?
3. Почему у метода не может быть своего параметра типа?
4. Чем `any` отличается от параметра типа `T any`?
5. Как устроен range-over-func? Что будет, если итератор не проверяет результат `yield`?

## Практика

- Задача [`04-patterns/lru`](../04-patterns/lru) — потокобезопасный LRU-кэш на дженериках.
- Упражнение: допиши `Stack[T]` из главы с методом `All() iter.Seq[T]` и тестом, где цикл прерывается `break`.

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Map, Filter, Reduce
// expect: [1 4 9 16 25]
// expect: [2 4]
// expect: 15
// expect: [1 2 3]
// hint: Результат Map сразу аллоцируй нужного размера: make([]R, len(s)) или make([]R, 0, len(s)).
// hint: Второй параметр типа R в Map нужен, чтобы менять тип элементов: []string → []int.
package main

import (
	"fmt"
	"strconv"
)

func Map[T, R any](s []T, f func(T) R) []R             { panic("TODO") }
func Filter[T any](s []T, keep func(T) bool) []T       { panic("TODO") }
func Reduce[T, A any](s []T, init A, f func(A, T) A) A { panic("TODO") }

func main() {
	nums := []int{1, 2, 3, 4, 5}
	fmt.Println(Map(nums, func(x int) int { return x * x }))
	fmt.Println(Filter(nums, func(x int) bool { return x%2 == 0 }))
	fmt.Println(Reduce(nums, 0, func(acc, x int) int { return acc + x }))
	fmt.Println(Map([]string{"1", "2", "3"}, func(s string) int { n, _ := strconv.Atoi(s); return n }))
}
```

```go
// exercise: Дженерик-множество
// expect: 3 true false
// expect: [go java rust]
// expect: [1 2 3]
// hint: Внутри — map[T]struct{}: пустая структура не занимает памяти.
// hint: Для Sorted нужен порядок, поэтому ограничение cmp.Ordered, а не comparable. slices.Sorted(maps.Keys(s.m)).
package main

import (
	"cmp"
	"fmt"
)

type Set[T cmp.Ordered] struct {
	m map[T]struct{}
}

func NewSet[T cmp.Ordered](items ...T) *Set[T] { panic("TODO") }
func (s *Set[T]) Add(v T)                      { panic("TODO") }
func (s *Set[T]) Has(v T) bool                 { panic("TODO") }
func (s *Set[T]) Len() int                     { panic("TODO") }
func (s *Set[T]) Sorted() []T                  { panic("TODO") }

func main() {
	s := NewSet("go", "rust", "go")
	s.Add("java")
	fmt.Println(s.Len(), s.Has("go"), s.Has("php"))
	fmt.Println(s.Sorted())
	fmt.Println(NewSet(3, 1, 2, 1).Sorted())
}
```

```go
// exercise: Ленивые итераторы
// expect: 0 4 16 36 64
// hint: iter.Seq[T] — это func(yield func(T) bool). Если yield вернул false, потребитель ушёл: сразу return.
// hint: Take считает отданные элементы и прекращает перебор источника, когда набрал n, — иначе бесконечный Naturals не остановится.
package main

import (
	"fmt"
	"iter"
)

// Naturals — бесконечная последовательность 0, 1, 2, ...
func Naturals() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; ; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func Filter[T any](seq iter.Seq[T], keep func(T) bool) iter.Seq[T] { panic("TODO") }
func Map[T, R any](seq iter.Seq[T], f func(T) R) iter.Seq[R]       { panic("TODO") }
func Take[T any](seq iter.Seq[T], n int) iter.Seq[T]               { panic("TODO") }

func main() {
	even := Filter(Naturals(), func(x int) bool { return x%2 == 0 })
	squares := Map(even, func(x int) int { return x * x })
	for v := range Take(squares, 5) {
		fmt.Print(v, " ")
	}
	fmt.Println()
}
```

## Самопроверка

- [ ] Напишу `Filter`, `Map`, `Reduce` и `Set[T comparable]` с методами.
- [ ] Объясню GC shape stenciling и следствия для производительности.
- [ ] Напишу итератор по своей структуре и объясню контракт `yield`.

Дальше: [Глава 8. Пакеты и дизайн кода](08-packages.md)
