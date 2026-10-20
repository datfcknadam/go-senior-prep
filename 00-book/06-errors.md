# Глава 6. Ошибки, panic и recover

## Зачем

В Go нет исключений: ошибка — обычное значение, и её обработка видна в коде. На собеседовании проверяют, умеешь ли ты строить
цепочки ошибок с контекстом, различать их через `errors.Is/As`, проводить границы между слоями и понимаешь ли, когда `panic` уместен.

---

## 1. error — это интерфейс 🟢

```go
type error interface {
    Error() string
}
```

```go
u, err := repo.Get(ctx, id)
if err != nil {
    return User{}, fmt.Errorf("get user %d: %w", id, err)
}
```

- Ошибка возвращается **последним** результатом. При ошибке остальные результаты обычно нулевые, и вызывающий их не использует.
- Ошибку проверяют **сразу**, до использования других результатов. Исключение — `io.Reader.Read`: он может вернуть `n > 0` вместе с `io.EOF`.
- Сообщения пишут строчными буквами, без точки в конце, потому что они склеиваются в цепочку: `create order: reserve item 42: db: timeout`.

## 2. Три вида ошибок 🟢

### Sentinel — заранее объявленное значение

```go
var ErrNotFound = errors.New("not found")

if errors.Is(err, ErrNotFound) { /* 404 */ }
```

Sentinel — часть **публичного API** пакета: переименовать или удалить его — ломающее изменение. Примеры из stdlib: `io.EOF`, `sql.ErrNoRows`, `context.Canceled`.

### Тип ошибки — когда нужны данные

```go
type ValidationError struct {
    Field  string
    Reason string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Reason }

var ve *ValidationError
if errors.As(err, &ve) {        // target — указатель на переменную типа ошибки
    respond(400, ve.Field)
}

if ve, ok := errors.AsType[*ValidationError](err); ok { // Go 1.26: дженерик-вариант
    respond(400, ve.Field)
}
```

### Непрозрачная ошибка — когда важно только поведение

Вызывающий проверяет `err != nil`, а свойства узнаёт через интерфейс поведения: `interface{ Timeout() bool }`. Так устроены сетевые ошибки (`net.Error`).

## 3. Обёртывание 🟢

```go
return fmt.Errorf("load profile %d: %w", id, err)   // оборачивает: цепочка сохраняется
return fmt.Errorf("load profile %d: %v", id, err)   // только текст: цепочка разорвана
return fmt.Errorf("both failed: %w, %w", err1, err2) // несколько %w, Go 1.20+
return errors.Join(errA, errB)                       // объединение нескольких ошибок
```

- `errors.Is(err, target)` идёт по цепочке `Unwrap()` и сравнивает **значения**. `errors.As` ищет **тип**.
- `%v` вместо `%w` — сознательный выбор, когда внутреннюю ошибку нельзя делать частью твоего API. Например, сервис не должен обещать
  клиентам, что наружу выйдет `pgx.ErrNoRows`.
- Свои правила сравнения задают методами `Is(target error) bool` и `As(any) bool`.

## 4. Ошибки на границах слоёв 🟡

```go
// repository: переводим ошибки драйвера в доменные
func (r *Repo) Get(ctx context.Context, id int64) (Ad, error) {
    var ad Ad
    err := r.db.QueryRow(ctx, `SELECT id, title FROM ads WHERE id = $1`, id).Scan(&ad.ID, &ad.Title)
    if errors.Is(err, pgx.ErrNoRows) {
        return Ad{}, domain.ErrNotFound
    }
    if err != nil {
        return Ad{}, fmt.Errorf("select ad %d: %w", id, err)
    }
    return ad, nil
}

// transport: переводим доменные ошибки в HTTP-коды — в одном месте
func httpStatus(err error) int {
    switch {
    case errors.Is(err, domain.ErrNotFound):
        return http.StatusNotFound
    case errors.As(err, new(*domain.ValidationError)):
        return http.StatusBadRequest
    case errors.Is(err, context.DeadlineExceeded):
        return http.StatusGatewayTimeout
    default:
        return http.StatusInternalServerError
    }
}
```

Правила senior-уровня:
- **Контекст на каждом уровне**, чтобы сообщение читалось как путь. Не повторяй то, что уже добавила вызванная функция.
- **Логируй один раз, на границе** (хендлер, воркер, консьюмер). «Залогировать и вернуть» даёт дубли в логах и шум.
- Доменный слой не знает про `pgx`, `net/http` и gRPC-коды.
- Клиенту не отдаётся текст внутренней ошибки (утечка деталей). Наружу идёт код и безопасное сообщение, внутрь лога — полная цепочка.

## 5. panic и recover 🟢

`panic` раскручивает стек горутины и выполняет её `defer`'ы. Если никто не вызвал `recover`, **падает весь процесс**.

```go
func safeGo(f func()) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                slog.Error("panic in goroutine", "panic", r, "stack", string(debug.Stack()))
                // + метрика
            }
        }()
        f()
    }()
}
```

- Когда `panic` уместен: нарушенный инвариант, ошибка программиста, невозможное состояние, `Must*`-функции в `init` и тестах (`regexp.MustCompile`).
- Когда **неуместен**: бизнес-ошибки, ошибки ввода, сети, БД. Библиотека не выпускает панику наружу.
- `recover()` работает, только если вызван **непосредственно** в отложенной функции **той горутины**, которая паникует.
  Recover в одной горутине не ловит панику в другой. Поэтому каждая фоновая горутина в сервисе запускается с recover-обёрткой.
- `net/http` сам ловит панику в хендлере и пишет её в лог (соединение рвётся), но твой код в `go func()` внутри хендлера он не защищает.
- `panic(nil)` с Go 1.21 превращается в `*runtime.PanicNilError`, поэтому `recover()` больше не возвращает nil при панике.
- **Фатальные ошибки рантайма не перехватываются:** конкурентная запись в мапу, нехватка памяти, переполнение стека, «all goroutines are asleep».

## Под капотом 🔴

- `panic` создаёт запись `_panic`, раскручивает стек и вызывает `_defer`'ы. Если defer вызвал `recover()`, паника помечается обработанной,
  и функция с этим defer'ом **возвращается нормально** с текущими значениями именованных результатов. Так паника превращается в ошибку:
  ```go
  func parse(s string) (res AST, err error) {
      defer func() {
          if r := recover(); r != nil {
              err = fmt.Errorf("parse: %v", r)
          }
      }()
      return mustParse(s), nil
  }
  ```
- `recover` проверяет, что его вызвали непосредственно из defer'а, который обрабатывает панику. Из вложенной функции он вернёт nil.
- `errors.Is` — цикл по `Unwrap`: сравнение через `==` (если тип сравним) и метод `Is`. Для `Unwrap() []error` обход идёт в глубину.
- `errors.As` принимает **указатель**, потому что должен записать найденное значение в твою переменную. Внутри — рефлексия и проверка присваиваемости типов.

## Ловушки

- Типизированный nil как `error` (глава 5).
- `if err != nil { log.Error(err); return err }` — двойное логирование.
- Сравнение `err == ErrNotFound` вместо `errors.Is`: не работает с обёрнутыми ошибками.
- `errors.As(err, ve)` вместо `errors.As(err, &ve)` — panic.
- Игнорирование ошибки `Close()` у записываемого файла или `rows.Err()` после цикла по строкам.
- Горутина без recover в сервисе: одна паника роняет все запросы.
- Ошибка оборачивается через `%w` с внутренними типами драйвера, и они становятся частью публичного API.

## Спросят

1. `errors.Is` и `errors.As`: зачем каждый? Почему `As` принимает указатель?
2. Чем `%w` отличается от `%v`? Когда сознательно выбирают `%v`?
3. Где логировать ошибку? Почему не «везде на всякий случай»?
4. Поймает ли `recover` в `main` панику из горутины?
5. Какие ошибки рантайма нельзя перехватить?

## Практика

- Задача [`01-language/errs`](../01-language/errs) — обёртки, `errors.Is/As`, свои типы ошибок.
- [PUZZLES.md](../01-language/PUZZLES.md): №12 «Recover».

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Ошибки на границе слоя
// expect: 200 ok
// expect: 404 get ad 42: not found
// expect: 400 create: validation: title is empty
// expect: 500 get ad 7: connection reset
// hint: getAd теряет цепочку: %v превращает ошибку в текст, и errors.Is уже ничего не найдёт. Нужен %w.
// hint: status: nil → 200; errors.Is(err, ErrNotFound) → 404; errors.As(err, &verr) → 400; иначе 500.
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return "validation: " + e.Field + " " + e.Msg }

func repoGet(id int) error {
	switch id {
	case 42:
		return ErrNotFound
	case 7:
		return errors.New("connection reset")
	}
	return nil
}

func getAd(id int) error {
	if err := repoGet(id); err != nil {
		return fmt.Errorf("get ad %d: %v", id, err)
	}
	return nil
}

func createAd(title string) error {
	if title == "" {
		return fmt.Errorf("create: %w", &ValidationError{"title", "is empty"})
	}
	return nil
}

// status переводит ошибку доменного слоя в HTTP-код.
func status(err error) int {
	panic("TODO")
}

func main() {
	for _, err := range []error{getAd(1), getAd(42), createAd(""), getAd(7)} {
		if err == nil {
			fmt.Println(status(err), "ok")
			continue
		}
		fmt.Println(status(err), err)
	}
}
```

```go
// exercise: Паника не роняет воркер
// expect: job 1: ok
// expect: job 2: panic: runtime error: integer divide by zero
// expect: job 3: ok
// hint: recover работает только в отложенной функции и только в той же горутине, где паника.
// hint: Именованный результат можно присвоить из defer: defer func() { if r := recover(); r != nil { err = ... } }()
package main

import "fmt"

// safeRun выполняет job и превращает панику в ошибку "panic: <значение>".
func safeRun(job func()) (err error) {
	job()
	return nil
}

func main() {
	jobs := []func(){
		func() {},
		func() { zero := 0; fmt.Println(1 / zero) },
		func() {},
	}
	for i, job := range jobs {
		if err := safeRun(job); err != nil {
			fmt.Printf("job %d: %v\n", i+1, err)
			continue
		}
		fmt.Printf("job %d: ok\n", i+1)
	}
}
```

```go
// exercise: Все ошибки валидации сразу
// expect: title: empty
// expect: price: must be positive
// expect: is empty: true
// expect: valid: true
// hint: errors.Join(errs...) вернёт nil, если все аргументы nil, а Error() склеит сообщения через \n.
// hint: Оборачивай через fmt.Errorf("title: %w", ErrEmpty) — errors.Is видит ErrEmpty сквозь Join и обёртку.
package main

import (
	"errors"
	"fmt"
)

var (
	ErrEmpty       = errors.New("empty")
	ErrNotPositive = errors.New("must be positive")
)

type Ad struct {
	Title string
	Price int
}

// validate возвращает все нарушения сразу, а не только первое.
func validate(a Ad) error {
	panic("TODO")
}

func main() {
	err := validate(Ad{Title: "", Price: -1})
	fmt.Println(err)
	fmt.Println("is empty:", errors.Is(err, ErrEmpty))
	fmt.Println("valid:", validate(Ad{Title: "bike", Price: 100}) == nil)
}
```

## Самопроверка

- [ ] Спроектирую ошибки для сервиса: sentinel, типы, маппинг в HTTP-коды на одной границе.
- [ ] Напишу безопасный запуск фоновой горутины с recover, логом и метрикой.
- [ ] Объясню, почему `errors.As` принимает `**T`, а не `*T`, для ошибки-указателя.

Дальше: [Глава 7. Дженерики и итераторы](07-generics.md)
