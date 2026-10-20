# Глава 5. Методы и интерфейсы

## Зачем

Интерфейсы — главный инструмент абстракции в Go и вопрос номер один на собеседовании («почему `err != nil`, хотя я вернул nil?»).
После главы ты сможешь объяснить поведение интерфейсов из их устройства и проектировать их так, как принято в Go.

---

## 1. Методы 🟢

Метод — функция с **ресивером**. Объявить метод можно у любого именованного типа из своего пакета, не только у структуры:

```go
type Money int64 // копейки

func (m Money) String() string { return fmt.Sprintf("%d.%02d ₽", m/100, m%100) }

type Wallet struct {
    mu      sync.Mutex
    balance Money
}

func (w *Wallet) Deposit(amount Money) { // ресивер-указатель: меняет состояние
    w.mu.Lock()
    defer w.mu.Unlock()
    w.balance += amount
}
```

### Значение или указатель 🟢

Ресивер-указатель нужен, если:
- метод **меняет** состояние;
- структура **большая** (копировать дорого);
- внутри есть то, что **нельзя копировать**: `sync.Mutex`, `atomic.*`, `noCopy`.

Ресивер-значение подходит для маленьких неизменяемых типов (`time.Time`, `Money`). **Внутри одного типа не смешивай**: если хоть один метод на указателе,
делай все на указателе.

- Вызов метода на `*T` у **адресуемого** значения компилятор переписывает сам: `w.Deposit(1)` → `(&w).Deposit(1)`.
  У значения в мапе или у результата функции адреса нет, поэтому `m[k].Deposit(1)` не компилируется.
- Метод с ресивером-указателем можно вызвать на nil-указателе, и это не panic, пока метод не обращается к полям. Иногда это используют специально.

### Method set 🟡

| Тип | Его методы |
|---|---|
| `T` | методы с ресивером `T` |
| `*T` | методы с ресивером `T` **и** `*T` |

Следствие: если `Deposit` объявлен на `*Wallet`, то `Wallet` (значение) интерфейс с `Deposit` **не реализует**, а `*Wallet` реализует.

## 2. Интерфейсы 🟢

Интерфейс — набор методов. Реализация **неявная**: тип реализует интерфейс, если у него есть все методы. Слова `implements` нет.

```go
type Notifier interface {
    Notify(ctx context.Context, userID int64, msg string) error
}

type SMS struct{ client *http.Client }

func (s *SMS) Notify(ctx context.Context, userID int64, msg string) error { /* ... */ return nil }

var _ Notifier = (*SMS)(nil) // проверка на этапе компиляции, ничего не стоит в рантайме
```

- Неявная реализация позволяет объявлять интерфейс **там, где он нужен** (у потребителя), а не там, где реализация.
  Пакет `storage` экспортирует конкретный `*PostgresRepo`, а сервис, которому нужен только `GetUser`, объявляет у себя `type userGetter interface{ GetUser(...) }`.
- **Accept interfaces, return structs:** принимай интерфейсы (гибкость и тестируемость), возвращай конкретные типы (вызывающий получает все методы и документацию).
- Интерфейсы маленькие: 1–3 метода. `io.Reader`, `io.Writer`, `fmt.Stringer`, `error`. Большие интерфейсы трудно реализовать и мокать.
- Не создавай интерфейс «на всякий случай» под единственную реализацию, если он не нужен для тестов или границы модуля.
- `any` = `interface{}`: значение любого типа. Нужен для действительно разнородных данных (JSON, логирование). Где можно, используй дженерики (глава 7).

### Встраивание интерфейсов

```go
type ReadWriter interface {
    io.Reader
    io.Writer
}
```

Интерфейс можно встроить и в **структуру**. Так частично реализуют большой интерфейс, например в моках: невызванные методы паникуют на nil.

```go
type fakeStore struct {
    Store                // встроенный интерфейс (nil)
    users map[int64]User
}

func (f *fakeStore) GetUser(_ context.Context, id int64) (User, error) { return f.users[id], nil }
```

## 3. Type assertion и type switch 🟢

```go
var r io.Reader = file
f := r.(*os.File)          // panic, если там не *os.File
f, ok := r.(*os.File)      // без паники

if fl, ok := w.(http.Flusher); ok { // опциональный интерфейс
    fl.Flush()
}

switch v := x.(type) {
case nil:
    return "nil"
case int, int64:
    return fmt.Sprint("число ", v) // v здесь имеет тип any: веток несколько
case string:
    return "строка " + v           // v здесь string
case fmt.Stringer:
    return v.String()
default:
    return fmt.Sprintf("%T", v)
}
```

Опциональные интерфейсы — мощный приём стандартной библиотеки: `io.Copy` проверяет `WriterTo`/`ReaderFrom`, `http.ResponseWriter` может оказаться `http.Flusher`.
Обратная сторона: обёртка над `ResponseWriter` в middleware «прячет» эти методы. Решение — `http.ResponseController` (1.20) или метод `Unwrap()`.

## 4. Nil-интерфейс ≠ интерфейс с nil-указателем 🟢

Это нужно знать до автоматизма.

```go
type MyErr struct{}

func (*MyErr) Error() string { return "boom" }

func find() error {
    var e *MyErr // nil-указатель
    return e     // интерфейс error = {тип: *MyErr, данные: nil}
}

err := find()
fmt.Println(err == nil) // false!
```

Интерфейс равен nil, только когда пусты **и тип, и данные**. Здесь тип записан (`*MyErr`), поэтому интерфейс не nil.

**Правило:** функция, возвращающая `error`, на успех пишет `return nil` литерально и никогда не возвращает типизированную nil-переменную.
То же касается любых интерфейсов: `var s *SMS; var n Notifier = s; n != nil`.

## Под капотом 🔴

```go
type iface struct { // непустой интерфейс: error, io.Reader
    tab  *itab      // (тип интерфейса, динамический тип, таблица методов)
    data unsafe.Pointer
}
type eface struct { // any
    _type *_type
    data  unsafe.Pointer
}
```

- **Что лежит в `data`.** Интерфейс — два слова, значение туда не помещается. Указатели, мапы, каналы, функции кладутся прямо в `data`.
  Остальное (int, string, структура) **копируется в кучу** через `runtime.convT*`. Без аллокации обходятся нулевые значения и однобайтовые числа (статическая таблица).
  Отсюда: `fmt.Println(x)` и `log(any)` в горячем пути — источник нагрузки на GC.
- **Значение в интерфейсе — копия.** Изменить исходную переменную через него нельзя.
- **Почему `T` не реализует интерфейс с методом на `*T`:** в `data` лежит анонимная копия `T`. Вызови на ней метод `*T`, и изменения молча потеряются.
  Язык запрещает это через method set. Обратное разрешено: для `*T` компилятор генерирует обёртку, которая разыменовывает указатель.
- **itab** строится статически, если пара (интерфейс, тип) известна при компиляции. Иначе (assertion из `any` в интерфейс) — в рантайме, с глобальным кешем.
  Методы отсортированы по имени, поэтому сопоставление идёт слиянием за O(n+m).
- **Стоимость операций:**

| Операция | Стоимость |
|---|---|
| вызов метода | загрузка из `tab.fun[i]` и косвенный вызов; без инлайна, если компилятор или PGO не девиртуализировал |
| `x.(*T)` | одно сравнение указателя |
| `x.(io.Reader)` | поиск или построение itab |
| `a == b` | сравнение типов, затем данных; несравнимый тип → panic |

- `err == nil` — это буквально `tab == nil && data == nil`. Поэтому «nil-интерфейс» не нужно запоминать, он следует из устройства.
- Подробнее: [INTERNALS.md §4](../01-language/INTERNALS.md#4-интерфейс).

## Ловушки

- Возврат типизированного nil как `error`.
- Методы на `T` и `*T` вперемешку: значение «то реализует, то нет».
- Копирование структуры с мьютексом через ресивер-значение. `go vet` (copylocks) это ловит.
- Интерфейс на 15 методов в пакете реализации «для DI».
- Обёртка `ResponseWriter` теряет `Flusher` и `Hijacker`.
- Встроенный nil-интерфейс в структуре: вызов нереализованного метода даёт panic.

## Спросят

1. Почему `err != nil`, хотя функция «вернула nil»? Объясни через устройство интерфейса.
2. Почему `T` не реализует интерфейс, если метод объявлен на `*T`?
3. Где объявлять интерфейс: рядом с реализацией или у потребителя? Почему?
4. Сколько стоит вызов через интерфейс? Аллоцирует ли упаковка значения в `any`?
5. Чем embedding отличается от наследования?

## Практика

- [PUZZLES.md](../01-language/PUZZLES.md): №3 «Nil-интерфейс», №13 «Копирование мьютекса», №15 «Сравнение интерфейсов».
- Упражнение: через `unsafe` разложи `any` на `{typ, data}` и проверь, что `var p *int; var a any = p` даёт `typ != nil`.
  Код есть в [INTERNALS.md §4](../01-language/INTERNALS.md#4-интерфейс).

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Nil, который не nil
// expect: anna: ok
// expect: bob: validation: age must be >= 18
// hint: return verr, где verr — nil-указатель *ValidationError, кладёт в error пару (тип, nil). Такой интерфейс != nil.
// hint: Когда ошибки нет, возвращай литерал nil.
package main

import "fmt"

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return "validation: " + e.Msg }

type User struct {
	Name string
	Age  int
}

func validate(u User) error {
	var verr *ValidationError
	if u.Age < 18 {
		verr = &ValidationError{Msg: "age must be >= 18"}
	}
	return verr
}

func main() {
	for _, u := range []User{{"anna", 30}, {"bob", 16}} {
		if err := validate(u); err != nil {
			fmt.Printf("%s: %v\n", u.Name, err)
			continue
		}
		fmt.Printf("%s: ok\n", u.Name)
	}
}
```

```go
// exercise: Type switch
// expect: int 42
// expect: string "hi"
// expect: error boom
// expect: stringer (1,2)
// expect: unknown bool
// hint: switch x := v.(type) { case int: ... } — в каждой ветке x уже нужного типа.
// hint: В case можно писать интерфейсы: case error, case fmt.Stringer. Для default пригодится %T.
package main

import (
	"errors"
	"fmt"
)

type Point struct{ X, Y int }

func (p Point) String() string { return fmt.Sprintf("(%d,%d)", p.X, p.Y) }

func describe(v any) string {
	panic("TODO")
}

func main() {
	for _, v := range []any{42, "hi", errors.New("boom"), Point{1, 2}, true} {
		fmt.Println(describe(v))
	}
}
```

```go
// exercise: Свой io.Writer
// expect: written=13 text="Hello, World!"
// hint: io.Writer — один метод Write(p []byte) (n int, err error). Допиши его и прибавляй len(p) к счётчику.
// hint: Ресивер должен быть указателем (*CountingWriter), иначе счётчик увеличится у копии.
package main

import (
	"fmt"
	"strings"
)

type CountingWriter struct {
	sb strings.Builder
	N  int
}

// TODO: реализуй io.Writer — сейчас программа не компилируется.

func main() {
	var w CountingWriter
	fmt.Fprintf(&w, "Hello, %s!", "World")
	fmt.Printf("written=%d text=%q\n", w.N, w.sb.String())
}
```

## Самопроверка

- [ ] Объясню method set и правило адресуемости.
- [ ] Нарисую `iface` и `eface` и объясню, когда упаковка в интерфейс аллоцирует.
- [ ] Спроектирую интерфейс для репозитория у потребителя и напишу для него fake в тесте.

Дальше: [Глава 6. Ошибки, panic и recover](06-errors.md)
