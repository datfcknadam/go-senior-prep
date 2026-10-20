# Глава 12. context и паттерны конкурентности

## Зачем

`context` связывает конкурентный код с жизненным циклом запроса: отмена, дедлайны, request-scoped данные. Паттерны — то,
что дают на live coding: worker pool, fan-in, pipeline, rate limiter, graceful shutdown. Каждый нужно уметь написать с нуля за 10–15 минут
и объяснить, почему нет гонок и утечек.

---

## 1. context 🟢

```go
type Context interface {
    Deadline() (deadline time.Time, ok bool)
    Done() <-chan struct{} // закрывается при отмене или дедлайне
    Err() error            // context.Canceled или context.DeadlineExceeded
    Value(key any) any
}
```

```go
ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
defer cancel()                       // ОБЯЗАТЕЛЬНО, даже если таймаут сработает сам
ad, err := repo.Get(ctx, id)
```

- Контексты образуют **дерево**. Отмена идёт от родителя ко всем потомкам, но не вверх.
- Корни: `context.Background()` в `main` и тестах (`t.Context()` с 1.24), `context.TODO()` — «ещё не решил, какой нужен».
- Производные: `WithCancel`, `WithTimeout`, `WithDeadline`, `WithValue`, `WithCancelCause` (1.20), `WithoutCancel` (1.21), `AfterFunc` (1.21).
- **`defer cancel()` обязателен.** Без него дочерний контекст висит в родителе и держит таймер до отмены родителя. `go vet` (lostcancel) это ловит.
- `context.Cause(ctx)` после `WithCancelCause` говорит, **почему** отменили, а не только «отменили».
- `context.WithoutCancel(ctx)` сохраняет значения, но не наследует отмену: например, записать аудит после того, как клиент уже получил ответ.
- `context.AfterFunc(ctx, f)` вызывает f при отмене. Удобно, чтобы прервать блокирующий вызов, который не понимает контекст.

### Правила использования 🟢

- `ctx` — **первый** параметр функции. Не храни его в структурах (исключение — объекты, живущие ровно один запрос). Не передавай `nil`.
- `Value` — только для **request-scoped** данных: trace id, user id, логгер с полями. Не для зависимостей (БД, конфиг) и не для опциональных параметров.
- Ключ `Value` — неэкспортируемый тип, чтобы не было коллизий между пакетами:
  ```go
  type ctxKey struct{}

  func WithUserID(ctx context.Context, id int64) context.Context {
      return context.WithValue(ctx, ctxKey{}, id)
  }

  func UserID(ctx context.Context) (int64, bool) {
      id, ok := ctx.Value(ctxKey{}).(int64)
      return id, ok
  }
  ```
- **Отмена в CPU-цикле** не происходит сама. Проверяй раз в N итераций:
  ```go
  if i%1024 == 0 {
      if err := ctx.Err(); err != nil {
          return err
      }
  }
  ```
- Дедлайн передаётся между сервисами: gRPC делает это автоматически, в HTTP — через заголовок с оставшимся бюджетом.

## 2. Worker pool 🟢

Фиксированное число воркеров обрабатывает поток задач. Это ограничивает параллелизм: узкое место — БД, сеть, внешний API, а не горутины.

```go
func Process(ctx context.Context, items []Item, workers int) ([]Result, error) {
    ctx, cancel := context.WithCancelCause(ctx)
    defer cancel(nil)

    jobs := make(chan int)
    results := make([]Result, len(items)) // порядок сохраняется: пишем по индексу

    var wg sync.WaitGroup
    for range workers {
        wg.Go(func() {
            for i := range jobs {
                r, err := handle(ctx, items[i])
                if err != nil {
                    cancel(err) // отмена для всех на первой ошибке
                    return
                }
                results[i] = r // разные индексы — без гонки
            }
        })
    }

loop:
    for i := range items {
        select {
        case jobs <- i:
        case <-ctx.Done():
            break loop
        }
    }
    close(jobs) // отправитель один — он и закрывает
    wg.Wait()

    if err := context.Cause(ctx); err != nil {
        return nil, err
    }
    return results, nil
}
```

Вопросы, которые зададут: как сохранить порядок, как остановиться на первой ошибке, что будет, если вызывающий отменил контекст,
почему `close(jobs)` безопасен, где тут happens-before между записью `results[i]` и чтением после `wg.Wait()`.

## 3. errgroup — самый практичный инструмент 🟢

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10)                       // не больше 10 одновременно
results := make([]Profile, len(ids))
for i, id := range ids {
    g.Go(func() error {
        p, err := client.Profile(ctx, id)
        if err != nil {
            return fmt.Errorf("profile %d: %w", id, err)
        }
        results[i] = p
        return nil
    })
}
if err := g.Wait(); err != nil {     // первая ошибка; ctx отменён для остальных
    return nil, err
}
```

`golang.org/x/sync/errgroup`. Результаты собирай сам: по индексу без мьютекса или в структуру под мьютексом.

## 4. Fan-out, fan-in, pipeline 🟡

- **Pipeline** — стадии, соединённые каналами. Каждая стадия читает вход, пишет в свой выход, **закрывает свой выход** и слушает `ctx`.
- **Fan-out** — несколько воркеров читают один канал, чтобы ускорить медленную стадию. **Fan-in** — слияние нескольких каналов в один.

```go
func Merge[T any](ctx context.Context, ins ...<-chan T) <-chan T {
    out := make(chan T)
    var wg sync.WaitGroup
    for _, in := range ins {
        wg.Go(func() {
            for v := range in {
                select {
                case out <- v:
                case <-ctx.Done():
                    return
                }
            }
        })
    }
    go func() {
        wg.Wait()
        close(out) // закрывает координатор, когда все отправители закончили
    }()
    return out
}
```

Пропускная способность конвейера определяется самой медленной стадией. Её масштабируют fan-out'ом. Буферы между стадиями сглаживают всплески.

## 5. Семафор 🟢

```go
sem := make(chan struct{}, 10)
for _, task := range tasks {
    select {
    case sem <- struct{}{}:       // занять слот
    case <-ctx.Done():
        return ctx.Err()
    }
    go func() {
        defer func() { <-sem }()  // освободить
        run(task)
    }()
}
```

Взвешенный семафор с контекстом: `golang.org/x/sync/semaphore`. Корректность держится на правиле happens-before для буферизованного канала (глава 11).

## 6. Rate limiting 🟡

- **Token bucket:** ведро ёмкостью B пополняется со скоростью r токенов в секунду, запрос забирает токен. Разрешает всплески до B.
  Реализация: `golang.org/x/time/rate` (`Allow`, `Wait(ctx)`, `Reserve`).
- **Leaky bucket** — ровный поток на выходе. **Fixed window** — простой счётчик за окно, но на границе окон пропускает двойной объём.
  **Sliding window** точнее.
- **Распределённый лимит** (на кластер, а не на инстанс): Redis + Lua-скрипт для атомарности, или алгоритм GCRA.
- Token bucket без горутины-пополнителя: при каждом запросе досчитывай токены по прошедшему времени. Так и проще, и тестируемее (время подменяется).

## 7. singleflight 🟡

Схлопывает одинаковые одновременные запросы в один: защита от **cache stampede**, когда истёк ключ популярного кеша и тысяча запросов разом идёт в БД.

```go
var g singleflight.Group
v, err, shared := g.Do("ad:"+id, func() (any, error) {
    return repo.Get(ctx, id)
})
```

Подвохи: если запрос завис, ждут все — нужен таймаут или `DoChan` с select на `ctx.Done()`. Контекст первого вызывающего влияет на всех:
если он отменился, ошибку получат все. Иногда для общей загрузки используют `context.WithoutCancel` + свой таймаут.

## 8. Graceful shutdown 🟢

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    srv := &http.Server{Addr: ":8080", Handler: router}
    g, gctx := errgroup.WithContext(ctx)

    g.Go(func() error {
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            return err
        }
        return nil
    })
    g.Go(func() error { return consumer.Run(gctx) }) // фоновые воркеры слушают ctx

    g.Go(func() error {
        <-gctx.Done()                                 // сигнал или падение соседа
        shCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        return srv.Shutdown(shCtx)                    // перестаёт принимать, ждёт активные запросы
    })

    if err := g.Wait(); err != nil {
        slog.Error("service stopped", "err", err)
    }
    // затем: flush метрик и логов, закрыть БД — в порядке, обратном запуску
}
```

В Kubernetes: при SIGTERM сначала падает readiness-проба, затем пауза на несколько секунд, чтобы балансировщики убрали pod из endpoints,
и только потом `Shutdown`. `terminationGracePeriodSeconds` должен быть больше суммарного времени остановки.

## Под капотом 🔴

- `cancelCtx` хранит мапу детей под мьютексом. `cancel()` закрывает `done`, записывает `err` и рекурсивно отменяет детей, затем удаляет себя из родителя.
  `timerCtx` дополнительно держит `time.Timer`.
- Если родитель — не стандартный контекст, `propagateCancel` запускает горутину, которая ждёт `parent.Done()`. Поэтому свои реализации `Context` дороже.
- `Value` ищет ключ **линейно вверх по цепочке** — O(глубины). Десятки `WithValue` в горячем пути заметны.
- `errgroup.WithContext` — это `WithCancelCause` + `sync.Once` для первой ошибки. `SetLimit` реализован семафором на канале.

## Ловушки

- Забытый `cancel()`.
- `context.Background()` внутри обработчика запроса вместо `r.Context()`: отмена клиента не дойдёт до БД.
- Хранение `ctx` в структуре сервиса.
- Зависимости и параметры в `ctx.Value`.
- Горутина на каждый элемент входного массива без лимита.
- Отправка результата без select на `ctx.Done()`: воркер зависнет, когда читатель ушёл.
- `srv.Shutdown` без таймаута или с отменённым контекстом сигнала.

## Спросят

1. Как устроена отмена в дереве контекстов? Что будет без `cancel()`?
2. Что можно класть в `context.Value`, а что нельзя?
3. Напиши worker pool с отменой на первой ошибке и сохранением порядка.
4. Как реализовать rate limiter? Чем token bucket лучше fixed window? А если инстансов 20?
5. Что такое cache stampede и как помогает singleflight? Какие у него подвохи?
6. Опиши graceful shutdown сервиса в Kubernetes по шагам.

## Практика

| Задача | Что тренирует |
|---|---|
| [`02-concurrency/workerpool`](../02-concurrency/workerpool) | пул с лимитом, порядок результатов, отмена при первой ошибке |
| [`02-concurrency/ratelimit`](../02-concurrency/ratelimit) | token bucket, тестируемое время, `Wait` с контекстом |
| [`02-concurrency/singleflight`](../02-concurrency/singleflight) | схлопывание одинаковых запросов |
| [`02-concurrency/merge`](../02-concurrency/merge) | fan-in с отменой |

Чек-лист ревью конкурентного кода — в [02-concurrency/THEORY.md §11](../02-concurrency/THEORY.md#11-чек-лист-ревью-конкурентного-кода).

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Таймаут на вызов
// expect: fast: ok
// expect: slow: context deadline exceeded
// expect: cancelled: context canceled
// hint: context.WithTimeout(ctx, timeout) — дочерний контекст, который отменится сам. Наследует и отмену родителя.
// hint: defer cancel() обязателен: иначе таймер и узел дерева контекстов живут до срабатывания таймаута.
package main

import (
	"context"
	"fmt"
	"time"
)

func slowCall(ctx context.Context, d time.Duration) (string, error) {
	select {
	case <-time.After(d):
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// withTimeout вызывает slowCall(ctx, d), но ждёт не дольше timeout.
func withTimeout(ctx context.Context, timeout, d time.Duration) (string, error) {
	panic("TODO")
}

func show(label, res string, err error) {
	if err != nil {
		fmt.Println(label+":", err)
		return
	}
	fmt.Println(label+":", res)
}

func main() {
	ctx := context.Background()
	res, err := withTimeout(ctx, 100*time.Millisecond, 10*time.Millisecond)
	show("fast", res, err)
	res, err = withTimeout(ctx, 30*time.Millisecond, time.Second)
	show("slow", res, err)

	parent, cancel := context.WithCancel(ctx)
	cancel()
	res, err = withTimeout(parent, time.Second, 10*time.Millisecond)
	show("cancelled", res, err)
}
```

```go
// exercise: Первая ошибка отменяет остальных
// expect: err: task 3: boom
// expect: finished fast: true
// expect: goroutines: 1
// flags: race
// hint: Это errgroup.WithContext из golang.org/x/sync, только руками: общий ctx с cancel, WaitGroup и «первая ошибка» под sync.Once.
// hint: Остальные задачи после отмены вернут context.Canceled — эту ошибку нельзя отдать наружу вместо настоящей.
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

func task(ctx context.Context, i int) error {
	if i == 3 {
		return fmt.Errorf("task %d: boom", i)
	}
	select {
	case <-time.After(time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runAll запускает task(ctx, i) для i из [0, n) параллельно, возвращает первую ошибку
// и сразу отменяет остальные задачи. Возвращается, только когда все горутины завершились.
func runAll(ctx context.Context, n int) error {
	panic("TODO")
}

func main() {
	start := time.Now()
	err := runAll(context.Background(), 10)
	fmt.Println("err:", err)
	fmt.Println("finished fast:", time.Since(start) < 500*time.Millisecond)
	fmt.Println("goroutines:", runtime.NumGoroutine())
}
```

```go
// exercise: Pipeline
// expect: 385
// flags: race
// hint: Каждая стадия: создаёт выходной канал, запускает горутину с defer close(out) и возвращает канал на чтение.
// hint: Любая отправка — в select с ctx.Done(), иначе при отмене стадии зависнут, держа друг друга.
package main

import (
	"context"
	"fmt"
)

func gen(ctx context.Context, nums ...int) <-chan int      { panic("TODO") }
func square(ctx context.Context, in <-chan int) <-chan int { panic("TODO") }

func sum(in <-chan int) int {
	total := 0
	for v := range in {
		total += v
	}
	return total
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmt.Println(sum(square(ctx, gen(ctx, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10))))
}
```

## Самопроверка

- [ ] Напишу worker pool, fan-in и семафор с нуля без подсказок и объясню их корректность.
- [ ] Опишу дерево контекстов и стоимость `Value`.
- [ ] Напишу `main` с graceful shutdown HTTP-сервера и консьюмера.

Дальше: [Глава 13. Планировщик GMP](13-scheduler.md)
