# Глава 9. Тестирование

## Зачем

Тестирование в Go встроено в тулчейн, и от senior ждут не «пишу тесты», а умения сделать код тестируемым: управляемое время,
зависимости через интерфейсы, тесты конкурентного кода под `-race`, бенчмарки с честным сравнением. Все практические задачи
этого репозитория проверяются тестами, так что глава нужна до части III.

---

## 1. Основы 🟢

```go
// money_test.go — файлы *_test.go не попадают в бинарник
package money

import "testing"

func TestFormat(t *testing.T) {
    got := Format(12345)
    if got != "123.45 ₽" {
        t.Errorf("Format(12345) = %q, want %q", got, "123.45 ₽")
    }
}
```

```bash
go test ./...                     # все пакеты
go test -run 'TestFormat/neg' -v  # по регулярке, с подтестом
go test -race -count=1 ./...      # с race-детектором, без кеша результатов
go test -cover -coverprofile=c.out && go tool cover -html=c.out
go test -shuffle=on               # случайный порядок тестов: ловит зависимость от порядка
```

- `t.Errorf` фиксирует провал и продолжает, `t.Fatalf` останавливает **текущий** тест (только из горутины теста, не из порождённой!).
- Формат сообщения: `Func(args) = got, want want`. Читатель сразу видит, что вызывали и что ожидали.
- `t.Helper()` в функции-помощнике: в сообщении будет строка вызывающего теста.
- `t.Cleanup(f)` — отложенная очистка, которая работает и из хелперов. `t.TempDir()`, `t.Setenv()`, `t.Context()` (1.24) упрощают изоляцию.
- Пакет `pkg_test` (внешний тест) видит только экспорт — так тестируют API глазами пользователя. Пакет `pkg` (внутренний) видит всё.

## 2. Табличные тесты и подтесты 🟢

```go
func TestParsePrice(t *testing.T) {
    tests := []struct {
        name    string
        in      string
        want    Money
        wantErr bool
    }{
        {name: "integer", in: "100", want: 10000},
        {name: "kopecks", in: "1.05", want: 105},
        {name: "negative", in: "-1", wantErr: true},
        {name: "garbage", in: "abc", wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel() // безопасно с Go 1.22: tt своя на каждой итерации
            got, err := ParsePrice(tt.in)
            if (err != nil) != tt.wantErr {
                t.Fatalf("ParsePrice(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("ParsePrice(%q) = %d, want %d", tt.in, got, tt.want)
            }
        })
    }
}
```

- Добавить случай — одна строка. Граничные значения перечислены явно и видны на ревью.
- Для сравнения структур — `github.com/google/go-cmp/cmp.Diff(want, got)`: читаемый дифф вместо «не равны».
- testify (`require`, `assert`) широко распространён в индустрии. Стандартная библиотека тоже нормальна; главное — единообразие.

## 3. Тестируемый дизайн: зависимости 🟡

Код тестируем, когда зависимости **передаются**, а не создаются внутри:

```go
type Clock interface{ Now() time.Time }

type PromoService struct {
    repo  PromoRepo // интерфейс, объявленный здесь, у потребителя
    clock Clock
}
```

Подмены бывают трёх видов:
- **fake** — рабочая упрощённая реализация (мапа вместо БД). Обычно лучший выбор: тесты проверяют поведение, а не вызовы;
- **stub** — возвращает заранее заданные ответы;
- **mock** — проверяет, что вызовы были сделаны (gomock/mockery). Полезен для проверки побочных эффектов (отправили событие ровно один раз),
  но тесты на моках хрупки: ломаются при рефакторинге без изменения поведения.

**Время** — главный источник нестабильных тестов. Варианты: интерфейс `Clock` или поле `now func() time.Time`, либо `testing/synctest` (ниже).
`time.Sleep` в тестах — признак проблемы.

## 4. HTTP и БД в тестах 🟡

```go
func TestHandler(t *testing.T) {
    h := NewHandler(fakeService{})
    req := httptest.NewRequest(http.MethodGet, "/ads/42", nil)
    rec := httptest.NewRecorder()

    h.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
    }
}

// клиент к внешнему API — против тестового сервера
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusServiceUnavailable)
}))
defer srv.Close()
client := NewClient(srv.URL)
```

- **Интеграционные тесты с БД** — настоящий Postgres в Docker через `testcontainers-go`, миграции перед тестами, транзакция с откатом
  или отдельная схема на тест. Моки SQL (`sqlmock`) проверяют текст запроса, а не корректность, поэтому для репозиториев они слабы.
- Разделение быстрых и медленных: `testing.Short()` + `go test -short`, либо build tag `//go:build integration`.
- `TestMain(m *testing.M)` — общая подготовка для пакета (поднять контейнер один раз).

## 5. Тесты конкурентного кода 🔴

- **Всегда `-race`.** Детектор видит только случившиеся гонки, поэтому тест должен реально запускать горутины конкурентно.
  ```go
  func TestWallet_ConcurrentWithdraw(t *testing.T) {
      w := NewWallet(100)
      var wg sync.WaitGroup
      for range 1000 {
          wg.Go(func() { _ = w.Withdraw(1) }) // Go 1.25
      }
      wg.Wait()
      if got := w.Balance(); got != 0 {
          t.Fatalf("balance = %d, want 0", got)
      }
  }
  ```
- **Утечки горутин:** `go.uber.org/goleak` (`defer goleak.VerifyNone(t)`) проверяет, что после теста не осталось горутин.
- **`testing/synctest` (Go 1.25):** тест выполняется в «пузыре» с виртуальным временем. Часы двигаются, только когда все горутины пузыря заблокированы,
  поэтому таймауты, ретраи и тикеры проверяются мгновенно и детерминированно.
  ```go
  func TestTimeout(t *testing.T) {
      synctest.Test(t, func(t *testing.T) {
          ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
          defer cancel()
          start := time.Now()
          <-ctx.Done()                  // реального ожидания нет
          if time.Since(start) != 5*time.Second {
              t.Fatal("virtual time mismatch")
          }
          synctest.Wait()               // дождаться, пока все горутины пузыря заблокируются
      })
  }
  ```
- `-count=100` и `-cpu=1,4,8` помогают выловить редкие гонки и ошибки, зависящие от планировщика.

## 6. Бенчмарки 🟡

```go
func BenchmarkJoinIDs(b *testing.B) {
    ids := makeIDs(1000)
    b.ReportAllocs()
    for b.Loop() { // Go 1.24+: защищает от удаления кода компилятором и сам управляет таймером
        JoinIDs(ids)
    }
}
```

```bash
go test -run '^$' -bench JoinIDs -benchmem -count=10 > old.txt
# ... изменение ...
go test -run '^$' -bench JoinIDs -benchmem -count=10 > new.txt
benchstat old.txt new.txt
```

- Смотри `ns/op`, `B/op`, `allocs/op`. В горячем пути сервиса **аллокации важнее наносекунд**, потому что превращаются в работу GC.
- Сравнивать результаты — только через `benchstat` (статистическая значимость), а не на глаз по одному прогону.
- Подробнее про профилирование — глава 15.

## 7. Fuzzing 🟡

```go
func FuzzParsePrice(f *testing.F) {
    f.Add("1.05")
    f.Add("-0")
    f.Fuzz(func(t *testing.T, s string) {
        p, err := ParsePrice(s)
        if err != nil {
            return
        }
        if back := FormatPrice(p); back != Normalize(s) { // свойство: round-trip
            t.Errorf("round trip %q → %d → %q", s, p, back)
        }
    })
}
```

`go test -fuzz=FuzzParsePrice -fuzztime=30s`. Найденные входы сохраняются в `testdata/fuzz/` и становятся регрессионными тестами.
Подходит для парсеров, декодеров и любых функций с инвариантами (round-trip, «не паникует»).

## 8. Что ещё полезно знать 🟢

- **Golden files:** ожидаемый вывод лежит в `testdata/*.golden`, флаг `-update` перезаписывает его. Удобно для больших JSON и шаблонов.
- **Example-тесты:** `func ExampleFormat() { fmt.Println(Format(5)) // Output: 0.05 ₽ }` — одновременно документация и тест.
- Каталог `testdata/` игнорируется тулчейном, туда кладут фикстуры.
- **Покрытие** — индикатор, а не цель. 100% покрытия без проверок граничных случаев ничего не гарантирует.

## Ловушки

- `t.Fatal` из порождённой горутины: тест не остановится корректно. Передавай ошибку через канал или используй `t.Error`.
- `time.Sleep` для «подождать, пока горутина отработает» — нестабильный тест.
- Общая мутабельная фикстура у параллельных подтестов.
- Бенчмарк, который компилятор оптимизировал до пустого цикла (до `b.Loop`).
- Моки на всё, включая собственные простые структуры: тест повторяет реализацию.
- Тесты без `-race` в CI.

## Спросят

1. Как сделать тестируемым код, который зависит от времени?
2. Mock, stub, fake: в чём разница, что ты предпочитаешь и почему?
3. Как ты тестируешь репозиторий, который работает с Postgres?
4. Как найти утечку горутин в тестах?
5. Как сравнить производительность двух реализаций, чтобы результат был честным?

## Практика

Все задачи репозитория — это тесты, которые нужно сделать зелёными:

```bash
make test-one T=./01-language/slices
go test -race -v ./02-concurrency/ratelimit   # здесь время уже сделано управляемым — посмотри, как
```

Прочитай тесты задачи [`02-concurrency/ratelimit`](../02-concurrency/ratelimit) и [`04-patterns/circuitbreaker`](../04-patterns/circuitbreaker):
как они подменяют время и проверяют конкурентное поведение.

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Управляемое время
// expect: 29 days: false
// expect: 31 days: true
// hint: Сервис не должен звать time.Now() напрямую — только s.clock.Now(). Тогда в тесте время двигается вручную.
// hint: fakeClock нужен метод Now() time.Time на указателе, чтобы изменения clock.now были видны сервису.
package main

import (
	"fmt"
	"time"
)

type Clock interface{ Now() time.Time }

type Ad struct{ CreatedAt time.Time }

type Service struct {
	clock Clock
	ttl   time.Duration
}

// IsExpired: объявление истекло, если с CreatedAt прошло больше ttl.
func (s *Service) IsExpired(a Ad) bool {
	panic("TODO")
}

// fakeClock — управляемые часы для тестов. TODO: реализуй Clock.
type fakeClock struct{ now time.Time }

func main() {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: base}
	svc := &Service{clock: clock, ttl: 30 * 24 * time.Hour}
	ad := Ad{CreatedAt: base}

	clock.now = base.AddDate(0, 0, 29)
	fmt.Println("29 days:", svc.IsExpired(ad))
	clock.now = base.AddDate(0, 0, 31)
	fmt.Println("31 days:", svc.IsExpired(ad))
}
```

```go
// exercise: Хендлер и httptest
// expect: 200 {"id":42,"title":"Велосипед"}
// expect: 404 ad not found
// expect: 400 bad id
// hint: Шаблон "GET /ads/{id}" (Go 1.22+) кладёт параметр в r.PathValue("id"); число — через strconv.Atoi.
// hint: Ошибки — http.Error(w, "bad id", http.StatusBadRequest). Успех — Content-Type и json.NewEncoder(w).Encode(...).
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

var ads = map[int]string{42: "Велосипед"}

func getAd(w http.ResponseWriter, r *http.Request) {
	// TODO: не число → 400 "bad id"; нет в ads → 404 "ad not found"; иначе 200 и {"id":..,"title":..}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ads/{id}", getAd)
	for _, path := range []string{"/ads/42", "/ads/7", "/ads/abc"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}
```

## Самопроверка

- [ ] Напишу табличный тест с подтестами и `t.Parallel()`.
- [ ] Сделаю время управляемым двумя способами.
- [ ] Напишу бенчмарк и сравню две версии через `benchstat`.
- [ ] Объясню, как работает `synctest` и почему он лучше `time.Sleep`.

Дальше: [Глава 10. Горутины, каналы, select](10-goroutines-channels.md)
