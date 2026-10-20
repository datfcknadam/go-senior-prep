# Глава 3. Управление потоком и функции

## Зачем

Синтаксис здесь простой, но именно в этой главе живут классические головоломки: замыкание в цикле, вычисление аргументов `defer`,
изменение именованного результата. На собеседовании их дают как разминку «что выведет».

---

## 1. if и switch 🟢

```go
if v, err := strconv.Atoi(s); err != nil { // v и err видны только в if/else
    return 0, fmt.Errorf("parse %q: %w", s, err)
} else if v < 0 {
    return 0, errNegative
}
```

- Скобок вокруг условия нет, фигурные скобки обязательны.
- Идиома «счастливый путь без отступа»: обработай ошибку и выйди (`return`), а основной код оставь на верхнем уровне. `else` после `return` не пишут.

```go
switch status {
case StatusActive, StatusPending: // несколько значений
    serve()
case StatusBlocked:
    deny()
    fallthrough // явный переход в следующую ветку — используется редко
default:
    log()
}

switch { // switch без выражения = цепочка if-else
case age < 18:
case age < 65:
}
```

- `break` в `switch` не нужен: ветка не проваливается в следующую. А вот `break` внутри `switch`, который стоит в `for`, выходит из **switch**,
  а не из цикла. Для выхода из цикла нужна метка.
- Type switch разобран в главе 5.

## 2. for — единственный цикл 🟢

```go
for i := 0; i < n; i++ {}   // классический
for cond {}                 // как while
for {}                      // бесконечный
for i := range 10 {}        // range по целому числу, Go 1.22+
for i, v := range slice {}  // индекс и КОПИЯ элемента
for k, v := range m {}      // порядок случаен
for i, r := range str {}    // байтовая позиция и руна
for v := range ch {}        // до закрытия канала
for v := range seq {}       // итератор-функция, Go 1.23+ (глава 7)
```

- `v` в `range` — **копия** элемента. Изменение `v` не меняет слайс. Для изменения нужен `s[i].Field = x`.
- Выражение в `range` вычисляется **один раз** до начала цикла. `for i := range s` по слайсу, который растёт внутри цикла, пройдёт только исходную длину.
- Метки для вложенных циклов:
  ```go
  outer:
      for _, row := range grid {
          for _, cell := range row {
              if cell == target {
                  break outer
              }
          }
      }
  ```

### Переменные цикла (Go 1.22) 🟡

До Go 1.22 переменная цикла была **одна на весь цикл**, и замыкания захватывали её общую:

```go
for _, v := range []int{1, 2, 3} {
    go func() { fmt.Println(v) }() // до 1.22: часто 3 3 3
}
```

С 1.22 каждая итерация получает **свою** переменную, и баг ушёл. Новая семантика включается версией `go` в `go.mod`, а не версией компилятора.
На собеседовании нужно рассказать обе части: что было, что изменилось и от чего это зависит. Старый фикс `v := v` в новом коде не нужен.

## 3. Функции 🟢

```go
func div(a, b int) (int, error) {         // несколько результатов
    if b == 0 {
        return 0, errors.New("division by zero")
    }
    return a / b, nil
}

func sum(nums ...int) int {               // variadic: nums — это []int
    total := 0
    for _, n := range nums {
        total += n
    }
    return total
}
sum(1, 2, 3)
sum(xs...)                                // передать слайс как variadic — без копии!

func parse(s string) (n int, err error) { // именованные результаты
    n, err = strconv.Atoi(s)
    return // «голый» return: допустим в коротких функциях, в длинных вредит читаемости
}
```

- Перегрузки и значений по умолчанию нет. Вместо них используют разные имена, структуру опций или функциональные опции:
  ```go
  type Option func(*Server)

  func WithTimeout(d time.Duration) Option { return func(s *Server) { s.timeout = d } }

  func NewServer(addr string, opts ...Option) *Server {
      s := &Server{addr: addr, timeout: 5 * time.Second} // дефолты
      for _, o := range opts {
          o(s)
      }
      return s
  }
  ```
- `sum(xs...)` передаёт **тот же** слайс. Если функция изменит `nums[0]`, изменится и `xs`.
- Функции — значения первого класса: их передают, возвращают, хранят в мапах. Тип функции: `func(int) error`.
  Нулевое значение функции — `nil`, вызов nil-функции даёт panic.

## 4. Замыкания 🟢

Замыкание — функция, которая ссылается на переменные из внешней области. Захватывается **переменная**, а не её значение:

```go
func counter() func() int {
    n := 0
    return func() int {
        n++ // n живёт, пока живёт замыкание (переезжает в кучу)
        return n
    }
}
c := counter()
c(); c()        // 1, 2
```

- Если замыкание запускается в горутине и пишет во внешнюю переменную, это гонка данных (глава 11).
- **Method value** `f := obj.Method` фиксирует ресивер в момент вычисления. Для ресивера-значения это копия, и последующие изменения `obj` она не увидит.

## 5. defer 🟢

`defer` откладывает вызов до выхода из функции. Применяется для освобождения ресурсов рядом с их захватом:

```go
f, err := os.Open(path)
if err != nil {
    return err
}
defer f.Close()

mu.Lock()
defer mu.Unlock()
```

Три правила, из которых выводятся все головоломки:

1. **Порядок LIFO:** отложенные вызовы выполняются в обратном порядке.
2. **Аргументы вычисляются сразу,** в момент `defer`, а тело выполняется при выходе:
   ```go
   x := 1
   defer fmt.Println(x)              // напечатает 1
   defer func() { fmt.Println(x) }() // напечатает 2: замыкание видит переменную
   x = 2
   ```
3. **Отложенная функция может менять именованные результаты:**
   ```go
   func f() (n int) {
       defer func() { n *= 2 }()
       return 5 // n = 5 → defer → n = 10 → функция вернёт 10
   }
   ```
   На этом держатся идиомы «откатить транзакцию при ошибке» и «обогатить ошибку контекстом»:
   ```go
   func (r *Repo) Transfer(ctx context.Context, from, to int64, amount int64) (err error) {
       tx, err := r.db.BeginTx(ctx, nil)
       if err != nil {
           return err
       }
       defer func() {
           if err != nil {
               _ = tx.Rollback()
           }
       }()
       // ...
       return tx.Commit()
   }
   ```

- `defer` в цикле копит вызовы до выхода из **функции**, а не из итерации. Тысяча открытых файлов до конца функции — баг. Тело цикла выносят в отдельную функцию.
- `os.Exit` и `log.Fatal` не выполняют `defer`.
- Ошибку `Close()` у файла, открытого на запись, игнорировать нельзя: данные могли не записаться. Используй именованный `err` и `errors.Join(err, f.Close())`.

## Под капотом 🔴

- **Open-coded defers (Go 1.14+).** Если в функции ≤ 8 `defer` и ни одного в цикле, компилятор вставляет вызовы прямо в точки выхода,
  а «какие defer'ы взведены» хранит в битовой маске на стеке. Стоимость почти как у обычного вызова, поэтому «defer медленный» — устаревший тезис.
- `defer` в цикле создаёт запись `_defer` в связном списке горутины: медленнее, и записи живут до выхода.
- `return x` компилируется в «записать x в слот результата → выполнить defer'ы → RET». Поэтому defer видит и может переписать результат.
- Захваченная переменная, которая переживает функцию, переезжает в кучу. Это видно в выводе `go build -gcflags=-m`: `moved to heap: n`.
- Подробнее: [INTERNALS.md §8](../01-language/INTERNALS.md#8-defer-и-panic).

## Ловушки

- `break` внутри `select` или `switch` в цикле не выходит из цикла.
- Изменение `v` в `for _, v := range s` не меняет слайс.
- `defer` в цикле с открытием файлов или `rows`.
- `defer resp.Body.Close()` до проверки `err`: при ошибке `resp == nil`, и будет panic.
- Голый `return` в длинной функции с именованными результатами: непонятно, что возвращается.

## Спросят

1. Когда вычисляются аргументы `defer`? Что выведет код с `defer fmt.Println(i)` в цикле?
2. Как `defer` может изменить возвращаемое значение? Где это применяется на практике?
3. Что изменилось с переменными цикла в Go 1.22, и от чего зависит поведение?
4. Сколько стоит `defer` сегодня?

## Практика

- [PUZZLES.md](../01-language/PUZZLES.md): №4 «defer и аргументы», №5 «defer и именованный результат», №6 «Замыкания в цикле», №8 «Range по слайсу и изменения».

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Счётчик на замыкании
// expect: 1 2 3
// expect: 1
// hint: Переменная, объявленная в counter и захваченная возвращаемой функцией, живёт между вызовами (и уезжает в кучу).
// hint: Каждый вызов counter() создаёт новую переменную — поэтому у b свой отсчёт.
package main

import "fmt"

func counter() func() int {
	panic("TODO")
}

func main() {
	a, b := counter(), counter()
	fmt.Println(a(), a(), a())
	fmt.Println(b())
}
```

```go
// exercise: Трассировка через defer
// expect: enter a
// expect: enter b
// expect: exit b
// expect: exit a
// hint: trace печатает enter сразу и возвращает функцию, которая напечатает exit.
// hint: В defer trace("a")() сам trace вызывается в момент defer — отложена только возвращённая функция.
package main

import "fmt"

func trace(name string) func() {
	panic("TODO")
}

func a() {
	// TODO: одна строка с defer
	b()
}

func b() {
	// TODO: одна строка с defer
}

func main() { a() }
```

```go
// exercise: Цепочка middleware
// expect: auth before
// expect: log before
// expect: handler
// expect: log after
// expect: auth after
// hint: Middleware — функция Handler → Handler. Оборачивать нужно с конца списка, чтобы первый оказался снаружи.
// hint: for i := len(mws) - 1; i >= 0; i-- { h = mws[i](h) }
package main

import "fmt"

type Handler func()
type Middleware func(Handler) Handler

func named(name string) Middleware {
	return func(next Handler) Handler {
		return func() {
			fmt.Println(name, "before")
			next()
			fmt.Println(name, "after")
		}
	}
}

// chain применяет middleware так, чтобы первый в списке выполнялся первым.
func chain(h Handler, mws ...Middleware) Handler {
	panic("TODO")
}

func main() {
	h := chain(func() { fmt.Println("handler") }, named("auth"), named("log"))
	h()
}
```

## Самопроверка

- [ ] Без запуска предскажу вывод любой комбинации `defer` + замыкание + именованный результат.
- [ ] Напишу функциональные опции для конструктора.
- [ ] Объясню, почему `defer` в цикле — ошибка, и как её исправить.

Дальше: [Глава 4. Составные типы](04-composite.md)
