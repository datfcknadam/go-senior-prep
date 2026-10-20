# Глава 16. Стандартная библиотека для сервисов

## Зачем

Стандартная библиотека Go закрывает большую часть нужд бэкенда: HTTP-сервер и клиент, JSON, время, логирование.
На собеседовании и в задачах на ревью постоянно встречаются одни и те же ошибки: клиент без таймаута, незакрытое тело ответа,
чтение тела без лимита, неправильная работа со временем. Эта глава о том, как пользоваться stdlib в продакшне.

---

## 1. io: Reader и Writer 🟢

```go
type Reader interface{ Read(p []byte) (n int, err error) }
type Writer interface{ Write(p []byte) (n int, err error) }
```

Два крошечных интерфейса связывают файлы, сеть, сжатие, шифрование, буферы и HTTP-тела в один конвейер:

```go
f, _ := os.Open("ads.json.gz")
defer f.Close()
gz, _ := gzip.NewReader(f)
dec := json.NewDecoder(gz) // читает потоком, не загружая весь файл в память
```

- Контракт `Read`: может вернуть `n > 0` **и** ошибку одновременно (в том числе `io.EOF`). Сначала обработай `n` байт, потом ошибку.
  Ручной цикл чтения пишут редко: есть `io.Copy`, `io.ReadAll`, `bufio.Scanner`, `io.ReadFull`.
- `io.ReadAll` на недоверенном входе опасен: клиент пришлёт 10 ГБ. Ограничивай: `io.LimitReader(r, max)` или `http.MaxBytesReader`.
- `bufio.Reader`/`Writer` снижают число syscall'ов. Не забудь `Flush()` у `bufio.Writer`.
- `bufio.Scanner` по умолчанию режет строки длиннее 64 КБ (ошибка `token too long`) — увеличь буфер через `sc.Buffer`.
- `io.Pipe`, `io.MultiWriter`, `io.TeeReader` — сборка потоков без промежуточных буферов.

## 2. HTTP-сервер 🟢

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /ads/{id}", h.getAd)            // Go 1.22+: метод и параметры пути
mux.HandleFunc("POST /promotions", h.createPromotion)

srv := &http.Server{
    Addr:              ":8080",
    Handler:           withRecover(withLogging(mux)),
    ReadHeaderTimeout: 5 * time.Second,  // защита от Slowloris
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      15 * time.Second,
    IdleTimeout:       60 * time.Second,
}
```

```go
func (h *Handler) getAd(w http.ResponseWriter, r *http.Request) {
    id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid id")
        return
    }
    ad, err := h.svc.Get(r.Context(), id) // контекст запроса: отменится, если клиент ушёл
    if err != nil {
        writeError(w, httpStatus(err), "...") // маппинг ошибок — глава 6
        return
    }
    writeJSON(w, http.StatusOK, ad)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status) // заголовки — ДО WriteHeader; после первого Write их менять поздно
    _ = json.NewEncoder(w).Encode(v)
}
```

- Каждый запрос обрабатывается в **своей горутине**. Хендлер обязан быть безопасным для конкурентного вызова: общее состояние — под синхронизацией.
- Сервер **без таймаутов** по умолчанию: `http.ListenAndServe(":8080", h)` уязвим к медленным клиентам. В продакшне всегда свой `http.Server`.
- `r.Context()` отменяется, когда клиент закрыл соединение или сервер делает `Shutdown`. Передавай его во все вызовы вниз.
- Тело запроса ограничивай: `r.Body = http.MaxBytesReader(w, r.Body, 1<<20)`.
- **Middleware** — функция `func(http.Handler) http.Handler`:
  ```go
  func withLogging(next http.Handler) http.Handler {
      return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
          start := time.Now()
          rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
          next.ServeHTTP(rw, r)
          slog.InfoContext(r.Context(), "http request",
              "method", r.Method, "path", r.URL.Path, "status", rw.status, "dur", time.Since(start))
      })
  }

  type statusRecorder struct {
      http.ResponseWriter
      status int
  }

  func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }
  func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter } // для ResponseController
  ```
- Сторонние роутеры (chi, echo, gin) популярны в компаниях, но после 1.22 для многих сервисов хватает `ServeMux`.

## 3. HTTP-клиент 🟢

```go
var client = &http.Client{
    Timeout: 3 * time.Second, // общий таймаут на запрос, включая чтение тела
    Transport: &http.Transport{
        MaxIdleConns:        200,
        MaxIdleConnsPerHost: 50, // по умолчанию 2 — узкое место при нагрузке на один хост
        IdleConnTimeout:     90 * time.Second,
    },
}

func (c *Client) Profile(ctx context.Context, id int64) (Profile, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/users/"+strconv.FormatInt(id, 10), nil)
    if err != nil {
        return Profile{}, err
    }
    resp, err := c.http.Do(req)
    if err != nil {
        return Profile{}, fmt.Errorf("get profile: %w", err)
    }
    defer resp.Body.Close() // ПОСЛЕ проверки err

    if resp.StatusCode != http.StatusOK {
        _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)) // дочитать, чтобы соединение вернулось в пул
        return Profile{}, fmt.Errorf("get profile: status %d", resp.StatusCode)
    }
    var p Profile
    if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
        return Profile{}, fmt.Errorf("decode profile: %w", err)
    }
    return p, nil
}
```

- `http.DefaultClient` и `http.Get` **без таймаута** — запрос может висеть вечно.
- **Клиент создаётся один раз и переиспользуется**: в нём пул соединений. Клиент на каждый запрос — утечка сокетов и лишние TLS-рукопожатия.
- Тело ответа **всегда** закрывают и, если хотят переиспользовать соединение, дочитывают.
- Не-2xx ответ — **не ошибка** для `Do`. Статус проверяют явно.

## 4. JSON 🟢

```go
type Ad struct {
    ID        int64     `json:"id"`
    Title     string    `json:"title"`
    Price     *int64    `json:"price,omitempty"` // указатель: отличаем «нет поля» от 0
    CreatedAt time.Time `json:"created_at"`      // RFC 3339
    internal  string    // не сериализуется
}

dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields() // строгий режим для входящих запросов
if err := dec.Decode(&req); err != nil { /* 400 */ }
```

- Сериализуются только **экспортируемые** поля.
- `omitempty` пропускает zero value. Для структур и `time.Time` он не работает; с 1.24 есть `omitzero`, который учитывает и их (и метод `IsZero`).
- Числа в `any` декодируются как `float64`. Большие id (> 2⁵³) теряют точность — используй типизированные структуры или `dec.UseNumber()`.
- Свои форматы — методы `MarshalJSON`/`UnmarshalJSON` (или `MarshalText`/`UnmarshalText` для простых типов вроде enum).
- `encoding/json` медленный из-за рефлексии. В горячем пути — кодогенерация или экспериментальный `encoding/json/v2` (1.25, `GOEXPERIMENT=jsonv2`).

## 5. time 🟢

```go
start := time.Now()
elapsed := time.Since(start)         // использует монотонные часы — корректно при переводе системного времени

deadline := time.Now().Add(24 * time.Hour)
t, err := time.Parse(time.RFC3339, "2026-10-14T12:00:00+03:00")
s := t.UTC().Format(time.RFC3339)    // формат задаётся эталонной датой 2006-01-02 15:04:05
```

- `time.Time` сравнивают методами `Equal`, `Before`, `After`, а не `==`: `==` сравнивает ещё и локацию и монотонную часть.
- Храни и передавай время в **UTC**, в локальную зону переводи только при отображении.
- `time.Duration` — это `int64` наносекунд. Таймауты и интервалы передавай как `Duration`, а не «число секунд».
- `time.Ticker` и `time.Timer` останавливай через `Stop()`. С 1.23 неостановленные таймеры собирает GC, но логика всё равно должна их останавливать.
- `time.After` в цикле select — новый таймер на итерацию (глава 10).
- Для тестов время подменяют (глава 9).

## 6. Логирование: log/slog 🟢

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
slog.SetDefault(logger)

slog.Info("promotion activated", "ad_id", adID, "user_id", userID)
log := logger.With("request_id", reqID)              // логгер с полями запроса
log.ErrorContext(ctx, "charge failed", "err", err)
logger.LogAttrs(ctx, slog.LevelInfo, "hot path", slog.Int64("id", id)) // без аллокаций на any
```

- Структурированные логи (JSON) обязательны для сервиса: их индексирует ELK или Loki.
- Логируй **один раз**, на границе (глава 6). Не логируй секреты, токены и персональные данные.
- `trace_id` из контекста добавляют в логи через свой `slog.Handler`, который читает `ctx` — так логи связываются с трейсами.
- Уровень логов меняют без рестарта через `slog.LevelVar`.

## 7. Прочее полезное 🟡

- `os/signal.NotifyContext` — graceful shutdown (глава 12).
- `crypto/rand` для токенов и ключей, `math/rand/v2` для jitter и тестов. Никогда не наоборот.
- `net/url` для сборки URL, `url.QueryEscape` против инъекций в параметры.
- `embed` (`//go:embed migrations/*.sql`) — файлы внутри бинарника.
- `os.Root` (1.24) — доступ к файлам строго внутри каталога, защита от path traversal.
- `expvar`, `runtime/metrics` — метрики рантайма без внешних библиотек.

## Ловушки

- `http.Get` и клиенты без таймаута. `http.Server` без `ReadHeaderTimeout`.
- `defer resp.Body.Close()` до проверки ошибки; тело не закрыто или не дочитано.
- Новый `http.Client` (или `Transport`) на каждый запрос.
- `io.ReadAll(r.Body)` без лимита.
- `w.Header().Set` после `w.WriteHeader` или `w.Write`.
- Деньги и id в JSON через `float64`.
- `time.Now()` в бизнес-логике без возможности подменить в тестах.

## Спросят

1. Что не так с `http.Get(url)` в продакшн-коде? Какие таймауты есть у клиента и у сервера?
2. Почему тело ответа надо закрыть и дочитать?
3. Как устроен middleware в `net/http`? Как узнать статус ответа в middleware?
4. Что произойдёт с запросом в БД, если клиент закрыл соединение?
5. Как правильно хранить и сравнивать время?

## Практика

- [CODE_REVIEW.md](../08-interview/CODE_REVIEW.md): №3 «HTTP-обработчик» — найди все проблемы из этой главы.
- [09-final-project](../09-final-project/README.md): HTTP-слой сервиса продвижения.

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: JSON-контракт
// expect: {"id":1,"title":"Велосипед","price":"1500.50"}
// expect: {"id":2,"title":"Самокат","price":"0.99","tags":["kids"]}
// expect: decoded: 2 Самокат 99 <nil>
// hint: Теги полей: `json:"id"`, `json:"tags,omitempty"`. Неэкспортируемые поля encoding/json не видит вовсе.
// hint: Деньги — в копейках (int64), а в JSON — строка: MarshalJSON на значении Money, UnmarshalJSON на указателе *Money.
package main

import (
	"encoding/json"
	"fmt"
)

// Money — сумма в копейках. В JSON — строка "1500.50", чтобы клиенты не теряли точность на float.
type Money int64

// TODO: MarshalJSON и UnmarshalJSON для Money.

type Ad struct {
	ID       int
	Title    string
	Price    Money
	Tags     []string // в JSON — только если не пусто
	internal string
}

func main() {
	for _, ad := range []Ad{
		{ID: 1, Title: "Велосипед", Price: 150050, internal: "x"},
		{ID: 2, Title: "Самокат", Price: 99, Tags: []string{"kids"}},
	} {
		b, err := json.Marshal(ad)
		if err != nil {
			fmt.Println("error:", err)
			return
		}
		fmt.Println(string(b))
	}

	var ad Ad
	err := json.Unmarshal([]byte(`{"id":2,"title":"Самокат","price":"0.99"}`), &ad)
	fmt.Println("decoded:", ad.ID, ad.Title, int64(ad.Price), err)
}
```

```go
// exercise: Клиент с таймаутом
// expect: fast: 200 pong
// expect: slow: timeout
// hint: http.DefaultClient без таймаута: зависший сервер держит горутину и соединение вечно. Создай &http.Client{Timeout: ...}.
// hint: Ошибку таймаута распознают через net.Error: var ne net.Error; errors.As(err, &ne) && ne.Timeout(). И не забудь resp.Body.Close().
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// get делает GET и возвращает статус и тело. Таймаут ошибки — отдельный результат "timeout".
func get(c *http.Client, url string) string {
	panic("TODO")
}

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprint(w, "pong")
	}))
	defer srv.Close()

	client := http.DefaultClient // TODO: клиент с таймаутом 200ms
	fmt.Println("fast:", get(client, srv.URL+"/fast"))
	fmt.Println("slow:", get(client, srv.URL+"/slow"))
}
```

```go
// exercise: Разбор лога потоком
// expect: errors: 3
// expect: top: payments (2)
// hint: bufio.Scanner читает io.Reader построчно и не грузит весь файл в память. После цикла проверь sc.Err().
// hint: strings.Fields(line) → [уровень сервис сообщение...]. Считай ERROR по сервисам в мапе.
package main

import (
	"fmt"
	"io"
	"strings"
)

const logs = `INFO search ok
ERROR payments timeout
WARN search slow
ERROR payments declined
ERROR delivery lost
INFO payments ok`

// analyze возвращает число ошибок и сервис, где их больше всего.
func analyze(r io.Reader) (total int, top string, topN int, err error) {
	panic("TODO")
}

func main() {
	total, top, n, err := analyze(strings.NewReader(logs))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("errors:", total)
	fmt.Printf("top: %s (%d)\n", top, n)
}
```

## Самопроверка

- [ ] Напишу HTTP-сервер со всеми таймаутами, middleware логирования и recover.
- [ ] Напишу HTTP-клиент к внешнему API с таймаутом, проверкой статуса и закрытием тела.
- [ ] Перечислю 5 ошибок с `net/http`, которые ищу на ревью.

Дальше: [Глава 17. Работа с базой данных из Go](17-database.md)
