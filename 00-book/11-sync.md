# Глава 11. sync, атомики, memory model

## Зачем

Каналы передают данные, а пакет `sync` защищает общее состояние. На собеседовании здесь проверяют три вещи: знаешь ли ты устройство
и цену примитивов, умеешь ли объяснить корректность через happens-before и находишь ли гонки и дедлоки на ревью.

---

## 1. Гонка данных 🟢

**Гонка данных** — конкурентный доступ к одной ячейке памяти, где хотя бы один доступ — запись, и между доступами нет синхронизации.

```go
var counter int
for range 1000 {
    go func() { counter++ }() // гонка: counter++ — это чтение, сложение, запись
}
```

- Гонка — неопределённое поведение, а не «иногда неточный счётчик». На составных значениях (интерфейс, слайс, строка) она даёт **порванное значение**:
  тип от одного, данные от другого, дальше segfault. «Безобидных» гонок в Go не бывает.
- `go test -race` и `go run -race` находят гонки, которые **случились** при прогоне. Замедление в 2–20 раз, память в 5–10 раз.
  В CI обязателен, в проде не включают.

## 2. Mutex 🟢

```go
type Counter struct {
    mu sync.Mutex
    m  map[string]int
}

func (c *Counter) Inc(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.m[key]++
}
```

- Мьютекс защищает **данные**, а не код. Держи его рядом с полями, которые он защищает, и документируй это.
- Критическая секция должна быть **короткой**: никакого IO, сетевых вызовов и блокирующих операций с каналами под локом.
- Мьютекс не реентерабелен: повторный `Lock` в той же горутине — дедлок.
- Структуру с мьютексом нельзя копировать: ресиверы — указатели (`go vet` проверяет это через copylocks).
- `TryLock` есть с 1.18, но почти всегда это признак плохого дизайна.

## 3. RWMutex 🟡

- Много читателей **или** один писатель. `RLock`/`RUnlock` для чтения, `Lock`/`Unlock` для записи.
- **Писатель в приоритете:** после вызова `Lock()` новые `RLock()` ждут. Поэтому **рекурсивный `RLock` может дать дедлок**,
  если между двумя вызовами пришёл писатель.
- Дороже `Mutex` (атомики на счётчике читателей, борьба за кеш-линию на многих ядрах). Выигрывает, только когда чтений гораздо больше
  **и** секция чтения заметно длинная. Для короткой секции обычный `Mutex` часто быстрее — проверяй бенчмарком.

## 4. WaitGroup, Once, Cond 🟢

```go
var wg sync.WaitGroup
for _, url := range urls {
    wg.Add(1)                 // ДО go, иначе Wait может проскочить раньше Add
    go func() {
        defer wg.Done()
        fetch(url)
    }()
}
wg.Wait()

// Go 1.25: то же самое без ручного Add/Done
for _, url := range urls {
    wg.Go(func() { fetch(url) })
}
wg.Wait()
```

- Счётчик ниже нуля даёт panic. Переиспользовать `WaitGroup` можно только после возврата `Wait`.
- `sync.Once.Do(f)` выполняет f ровно один раз, и **все** вызвавшие дожидаются её завершения. Если f запаниковала, Once всё равно считается выполненным.
- `sync.OnceValue(f)`, `sync.OnceValues(f)`, `sync.OnceFunc(f)` (1.21) — удобная ленивая инициализация:
  ```go
  var loadConfig = sync.OnceValues(func() (*Config, error) { return parseConfig("cfg.yaml") })
  cfg, err := loadConfig()
  ```
- `sync.Cond` — ожидание условия: `for !cond { c.Wait() }`, всегда в цикле и под локом. На практике его почти всегда заменяет канал.

## 5. sync.Pool 🟡

Кеш временных объектов для снижения аллокаций: буферы, энкодеры.

```go
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func render(v any) []byte {
    buf := bufPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer func() {
        if buf.Cap() <= 64<<10 { // огромные буферы не возвращаем — пул раздуется
            bufPool.Put(buf)
        }
    }()
    _ = json.NewEncoder(buf).Encode(v)
    return bytes.Clone(buf.Bytes()) // нельзя отдать buf.Bytes(): буфер вернётся в пул
}
```

- Объекты могут исчезнуть в любой момент (при GC). Пул — **не** пул соединений и не кеш данных.
- Клади указатели: значение в `any` при `Put` аллоцирует.
- Не возвращай наружу ссылки на объект, который положишь обратно.

## 6. sync.Map 🟡

Оптимизирован под два сценария: ключ пишется один раз и много читается (кеш, реестр), либо горутины работают с **непересекающимися** наборами ключей.
В остальных случаях `map` + `Mutex` или шардированная мапа быстрее и типобезопаснее. С 1.24 внутри concurrent hash-trie.

**Шардирование** — частый ответ на «как убрать contention»:

```go
var seed = maphash.MakeSeed()

type ShardedMap[V any] struct {
    shards [64]struct {
        sync.RWMutex
        m map[string]V
    }
}

func (s *ShardedMap[V]) shard(key string) int {
    return int(maphash.String(seed, key) % 64)
}
```

## 7. Атомики 🟡

```go
var requests atomic.Int64
requests.Add(1)
n := requests.Load()

var cfg atomic.Pointer[Config]
cfg.Store(newCfg)        // писатель собирает новый объект целиком
c := cfg.Load()          // читатели — без локов
```

- Типы `atomic.Int64`, `atomic.Uint32`, `atomic.Bool`, `atomic.Pointer[T]`, `atomic.Value`. Операции `Load`, `Store`, `Add`, `Swap`, `CompareAndSwap`, с 1.23 `And`/`Or`.
- Атомики в Go **последовательно согласованы** (seq-cst). Слабых порядков памяти, как в C++, нет.
- **CAS-цикл** для операций, которых нет готовыми:
  ```go
  for {
      old := maxSeen.Load()
      if v <= old || maxSeen.CompareAndSwap(old, v) {
          break
      }
  }
  ```
- **Copy-on-write конфиг** через `atomic.Pointer` — популярный ответ на «как обновлять правила без блокировок».
- Атомик защищает **одну** переменную. Инвариант между двумя переменными требует мьютекса.
- **False sharing:** два горячих атомика в одной кеш-линии (64 байта), которые пишутся с разных ядер, мешают друг другу. Лечится паддингом.

## 8. Memory model 🔴

Модель памяти (пересмотрена в 1.19) отвечает на вопрос: когда запись в одной горутине гарантированно видна чтению в другой.

- **DRF-SC:** программа без гонок данных ведёт себя последовательно согласованно. С гонками гарантий нет.
- Отношение **happens-before** дают:
  - `go f()` happens-before начала `f`;
  - отправка в канал happens-before завершения соответствующего приёма;
  - `close(ch)` happens-before приёма, вернувшего zero value из-за закрытия;
  - для **небуферизованного** канала приём happens-before завершения отправки;
  - для буферизованного ёмкости C: k-й приём happens-before завершения (k+C)-й отправки — так работает семафор на канале;
  - `Unlock` happens-before следующего `Lock`;
  - завершение `f` в `Once.Do(f)` happens-before возврата любого `Do`;
  - если атомарный `Load` увидел значение `Store`, то `Store` happens-before `Load`.
- Классические ошибки:
  ```go
  var done bool
  go func() { work(); done = true }()
  for !done {} // может крутиться вечно: компилятор вправе вынести чтение из цикла
  ```
  ```go
  if instance == nil { // double-checked locking без атомика — гонка
      mu.Lock()
      if instance == nil {
          instance = newT()
      }
      mu.Unlock()
  }
  ```
  Правильные варианты: `sync.OnceValue` или `atomic.Pointer`.

## 9. Дедлоки 🟡

- Рантайм ловит только **тотальный** дедлок: `fatal error: all goroutines are asleep - deadlock!`. Если жива хоть одна горутина
  (HTTP-сервер, таймер), частичный дедлок не диагностируется, сервис просто «зависает».
- Типичные причины:
  - два мьютекса захватываются в разном порядке. Лечится **глобальным порядком захвата**, например по id (задача `bank`);
  - отправка в канал под мьютексом, пока получатель ждёт этот мьютекс;
  - рекурсивный `RLock` при ожидающем писателе;
  - `wg.Wait()` внутри горутины, которую сам и ждёшь.
- Диагностика в проде: `pprof/goroutine?debug=2` (стеки и время ожидания), профили `mutex` и `block`.

## Под капотом 🔴

- **Mutex** — `int32` состояния (locked, woken, starving, счётчик ждущих) + семафор рантайма.
  - **Нормальный режим:** пришедшая горутина может перехватить мьютекс у разбуженной: она уже на CPU, и это выгодно для throughput. Перед парковкой возможен короткий спин.
  - **Режим голодания:** если ждущий ждёт > 1 мс, мьютекс передаётся строго по очереди, новые горутины встают в хвост. Выход — когда ждущий последний или ждал < 1 мс.
- **Once:** fast path — атомарная проверка флага, slow path — мьютекс. Флаг ставится **после** выполнения f, поэтому все ждут окончания.
- **Pool:** локальный пул на каждый P (без локов) + кража у соседей + victim cache: при GC содержимое переезжает в victim, а при следующем GC пропадает.
- **Race detector** построен на ThreadSanitizer: векторные часы на каждую горутину и теневая память для каждого 8-байтного слова.
- Подробнее: [02-concurrency/THEORY.md §5–9](../02-concurrency/THEORY.md#5-пакет-sync).

## Ловушки

- `wg.Add` внутри горутины.
- Копирование структуры с мьютексом (ресивер-значение, `range` по слайсу структур).
- IO под мьютексом.
- `append` в общий слайс из нескольких горутин без лока. Если индексы известны, пиши в `results[i]` — это безопасно без лока.
- `atomic` на двух связанных полях вместо мьютекса.
- `RWMutex` «для скорости» без бенчмарка.

## Спросят

1. Что такое гонка данных и почему она не бывает безобидной?
2. Нормальный режим и режим голодания мьютекса. Почему по умолчанию нечестный режим?
3. Когда `RWMutex` хуже `Mutex`? Как получить дедлок на `RLock`?
4. Для чего `sync.Map`, а когда лучше шардированная мапа?
5. Объясни happens-before для буферизованного канала. Как на этом работает семафор?
6. Как обновлять конфигурацию без блокировок?
7. Как избежать дедлока при переводе денег между двумя счетами?

## Практика

- Задача [`02-concurrency/bank`](../02-concurrency/bank) — дедлок при взаимной блокировке и порядок захвата.
- Задача [`02-concurrency/singleflight`](../02-concurrency/singleflight) — схлопывание запросов на мьютексе и `WaitGroup`.
- [PUZZLES.md](../01-language/PUZZLES.md): №11 «Горутины и WaitGroup», №13 «Копирование мьютекса».

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Счётчик без гонки
// expect: 1000
// flags: race
// hint: counter++ — это чтение, сложение и запись. Две горутины читают одно значение — инкремент теряется.
// hint: Два честных варианта: sync.Mutex вокруг инкремента или atomic.Int64 с Add(1). Какой дешевле и почему?
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	counter := 0
	for range 1000 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++
		}()
	}
	wg.Wait()
	fmt.Println(counter)
}
```

```go
// exercise: Ленивая инициализация один раз
// expect: loads: 1
// expect: dsn: postgres://app
// flags: race
// hint: Проверка cfg == nil и присваивание — гонка: несколько горутин одновременно видят nil и грузят конфиг.
// hint: sync.Once, а с Go 1.21 — sync.OnceValue(loadConfig) возвращает функцию, которая вызовет loadConfig ровно один раз.
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct{ DSN string }

var loads atomic.Int32

func loadConfig() *Config {
	loads.Add(1)
	time.Sleep(10 * time.Millisecond) // читаем файл, ходим в vault...
	return &Config{DSN: "postgres://app"}
}

var cfg *Config

func getConfig() *Config {
	if cfg == nil {
		cfg = loadConfig()
	}
	return cfg
}

func main() {
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			getConfig()
		}()
	}
	wg.Wait()
	fmt.Println("loads:", loads.Load())
	fmt.Println("dsn:", getConfig().DSN)
}
```

```go
// exercise: Перевод без дедлока
// expect: a=100 b=100 total=200
// flags: race
// hint: transfer(a, b) берёт a, потом b; transfer(b, a) — b, потом a. Каждая ждёт мьютекс, который держит другая.
// hint: Бери блокировки в глобальном порядке — например, по возрастанию ID, независимо от направления перевода.
package main

import (
	"fmt"
	"sync"
)

type Account struct {
	ID      int
	mu      sync.Mutex
	balance int
}

func transfer(from, to *Account, amount int) {
	from.mu.Lock()
	defer from.mu.Unlock()
	to.mu.Lock()
	defer to.mu.Unlock()
	from.balance -= amount
	to.balance += amount
}

func main() {
	a, b := &Account{ID: 1, balance: 100}, &Account{ID: 2, balance: 100}
	var wg sync.WaitGroup
	for range 1000 {
		wg.Add(2)
		go func() { defer wg.Done(); transfer(a, b, 1) }()
		go func() { defer wg.Done(); transfer(b, a, 1) }()
	}
	wg.Wait()
	fmt.Printf("a=%d b=%d total=%d\n", a.balance, b.balance, a.balance+b.balance)
}
```

## Самопроверка

- [ ] Найду гонку в коде на ревью и объясню через happens-before, почему фикс корректен.
- [ ] Напишу шардированную мапу и copy-on-write конфиг.
- [ ] Расскажу режимы мьютекса и устройство `sync.Pool`.

Дальше: [Глава 12. context и паттерны конкурентности](12-context-patterns.md)
