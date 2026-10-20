# Глава 18. Архитектура сервиса

## Зачем

Senior отвечает не только за код функции, но и за то, как устроен сервис целиком: слои и зависимости, конфигурация, наблюдаемость,
взаимодействие с другими сервисами через HTTP, gRPC и Kafka. На собеседовании это всплывает в вопросах «расскажи про свой сервис»,
на ревью и на system design.

---

## 1. Слои и направление зависимостей 🟡

```
transport (HTTP, gRPC, Kafka consumer)
      │  DTO ⇄ доменные типы, коды ошибок
      ▼
service / use cases (бизнес-сценарии, транзакционные границы)
      │  интерфейсы, объявленные ЗДЕСЬ
      ▼
domain (сущности, правила, доменные ошибки — без зависимостей)
      ▲
storage, clients (Postgres, Redis, внешние API) — реализуют интерфейсы сервисного слоя
```

- Зависимости направлены **внутрь**: домен не знает про HTTP, SQL и Kafka. Это и есть суть «чистой» или «гексагональной» архитектуры, без лишних церемоний.
- Интерфейсы объявляет **потребитель** (сервисный слой), реализует инфраструктура (глава 5). Так нет циклических импортов, а сервис тестируется с fake.
- DTO транспорта ≠ доменная модель ≠ строка таблицы. Маппинг между ними — на границах. Для маленьких сервисов допустимо сократить число моделей,
  но доменная логика не должна зависеть от JSON-тегов и колонок.
- Не переусложняй: три слоя на CRUD из двух ручек — избыточно. Уровень абстракции растёт вместе со сложностью домена.

## 2. Dependency injection 🟡

В Go DI — это просто **передача зависимостей в конструктор**. Вся сборка в одном месте — `main` (composition root):

```go
func run(ctx context.Context) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }
    pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
    if err != nil {
        return err
    }
    defer pool.Close()

    repo := postgres.NewPromoRepo(pool)
    billing := billingclient.New(cfg.BillingURL, httpClient)
    svc := service.NewPromo(repo, billing, clock.Real{})
    h := httptransport.NewHandler(svc)

    return serve(ctx, cfg.Addr, h)
}

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    if err := run(ctx); err != nil {
        slog.Error("fatal", "err", err)
        os.Exit(1)
    }
}
```

- `run() error` вместо логики в `main`: можно использовать `defer` и возвращать ошибки, а `os.Exit` — один раз в конце.
- Контейнеры DI (wire, fx, dig) встречаются, но ручная сборка в Go — норма и чаще проще.
- Никаких глобальных переменных для БД, логгера и конфига.

## 3. Транзакции через несколько репозиториев 🔴

Сервисному слою нужно атомарно вызвать два репозитория, но он не должен знать про `pgx.Tx`. Распространённое решение — **transactor**:

```go
// service слой
type Transactor interface {
    WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

func (s *Promo) Buy(ctx context.Context, cmd BuyCmd) error {
    return s.tx.WithinTx(ctx, func(ctx context.Context) error {
        if err := s.wallets.Debit(ctx, cmd.UserID, cmd.Price); err != nil {
            return err
        }
        return s.promos.Create(ctx, cmd.AdID, cmd.UserID)
    })
}
```

Реализация в storage кладёт `pgx.Tx` в контекст, а репозитории берут из контекста транзакцию, если она есть, иначе пул.
Это одно из немногих оправданных использований `context.Value` для инфраструктуры. Альтернатива — передавать `Querier` (общий интерфейс `pool` и `tx`) явно.

## 4. Конфигурация 🟢

- Источник — **переменные окружения** (12-factor), секреты — из секрет-хранилища (Vault) или Kubernetes Secrets, не из репозитория.
- Конфиг читается **один раз** в `main`, валидируется при старте (fail fast) и передаётся в конструкторы как структура. Пакеты не читают env сами.
- Библиотеки: `caarlos0/env`, `kelseyhightower/envconfig`, `viper` (тяжеловат).
- Динамические настройки (фичефлаги, лимиты) обновляются без рестарта через `atomic.Pointer[Config]` (глава 11).

## 5. Наблюдаемость 🟡

Три сигнала:

| Сигнал | Что | Инструменты |
|---|---|---|
| **Метрики** | агрегаты во времени: RPS, ошибки, латентность, насыщение | Prometheus + Grafana, `prometheus/client_golang` |
| **Логи** | отдельные события с контекстом | `slog` в JSON → ELK / Loki |
| **Трейсы** | путь одного запроса через сервисы | OpenTelemetry → Jaeger / Tempo |

- **RED** для каждого эндпоинта и зависимости: Rate, Errors, Duration (гистограмма, а не среднее). **USE** для ресурсов: Utilization, Saturation, Errors
  (пул соединений, очереди, горутины).
- Латентность — **гистограммой**, по ней строят p50/p95/p99. Среднее скрывает хвосты.
- **Кардинальность меток** ограничена: `user_id` или полный URL в метке метрики взорвут Prometheus. Используй шаблон маршрута (`/ads/{id}`).
- `trace_id` пробрасывается через контекст и заголовки (W3C `traceparent`) и пишется в каждую строку лога — так лог связывается с трейсом.
- Метрики рантайма Go (горутины, GC, память) регистрируются коллектором `collectors.NewGoCollector()`.
- **Health-пробы:** `liveness` — процесс жив (не проверяет зависимости, иначе падение БД перезапустит все поды), `readiness` — готов принимать трафик.
- SLO и алерты по симптомам (доля ошибок, p99), а не по причинам (CPU 80%).

## 6. Межсервисное взаимодействие 🟡

### HTTP/JSON vs gRPC

| | REST/JSON | gRPC |
|---|---|---|
| контракт | OpenAPI (опционально) | `.proto` — строгий, с кодогенерацией |
| формат | текст, медленнее | protobuf, компактнее и быстрее |
| транспорт | HTTP/1.1 или 2 | HTTP/2, стриминг в обе стороны |
| дедлайны | вручную | встроенный deadline propagation |
| где | публичные API, фронтенд | внутренние вызовы между сервисами |

- В gRPC обратная совместимость держится на **номерах полей**: не переиспользуй номера, не меняй типы, добавляй новые поля, удалённые помечай `reserved`.
- gRPC-ошибки — коды `codes.NotFound`, `codes.Unavailable` и т. д. Маппинг доменных ошибок — на границе, как и для HTTP.
- Interceptor'ы gRPC — аналог middleware: логирование, метрики, recover, трейсинг, аутентификация.

### Асинхронно: Kafka 🟡

- Топик делится на **партиции**. Порядок гарантирован только **внутри партиции**, поэтому ключ сообщения (например, `ad_id`) определяет, какие события упорядочены.
- **Consumer group:** каждая партиция читается одним консьюмером группы. Параллелизм ≤ числу партиций.
- Доставка по умолчанию **at-least-once**: коммит offset после обработки, при сбое сообщение придёт снова. Значит, **консьюмер обязан быть идемпотентным**:
  таблица обработанных `message_id` с уникальным ключом (inbox) или идемпотентная операция (`INSERT ... ON CONFLICT DO NOTHING`).
- Go-клиенты: `segmentio/kafka-go`, `twmb/franz-go`, `confluent-kafka-go` (cgo).
- Консьюмер в Go: цикл чтения с `ctx`, обработка (возможно, пулом воркеров с сохранением порядка по ключу), коммит, graceful shutdown с дообработкой.

### Transactional outbox 🔴

Проблема: нужно записать в БД **и** отправить событие в Kafka атомарно. «Записать, потом отправить» теряет событие при падении между шагами,
«отправить, потом записать» отправляет событие о несуществующем изменении.

Решение: событие пишется в таблицу `outbox` **в той же транзакции**, что и бизнес-данные. Отдельный процесс читает `outbox`
(`SELECT ... FOR UPDATE SKIP LOCKED LIMIT 100`), публикует в Kafka и помечает отправленным. Либо CDC (Debezium) читает WAL.
Результат — at-least-once, поэтому консьюмеры идемпотентны.

## 7. Кеширование 🟡

- **Cache-aside:** прочитать кеш → промах → прочитать БД → положить в кеш с TTL. Самый частый вариант.
- **Инвалидация:** удалить ключ после записи в БД (а не обновить — меньше гонок). TTL — страховка от рассинхронизации.
- **TTL с jitter**, чтобы ключи не истекали одновременно. **Stampede** на популярном ключе — singleflight (глава 12) или ранний асинхронный рефреш.
- **Локальный кеш** (в памяти пода) быстрее Redis, но у каждого пода свой и инвалидация сложнее. Часто комбинируют: короткий локальный TTL + Redis.
- Горячие ключи в Redis Cluster упираются в один шард: локальный кеш или реплики ключа.

## Ловушки

- Доменный слой импортирует `pgx` или `net/http`.
- Глобальные синглтоны для БД и логгера.
- Liveness-проба проверяет базу.
- `user_id` в метках Prometheus.
- Публикация в Kafka после коммита без outbox — потеря событий при падении.
- Неидемпотентный консьюмер при at-least-once.
- Ломающее изменение `.proto` (переиспользован номер поля).

## Спросят

1. Расскажи архитектуру своего сервиса. Почему так, что бы ты изменил?
2. Как ты организуешь транзакцию, которая затрагивает несколько репозиториев?
3. Какие метрики ты добавишь новому сервису в первую очередь?
4. Чем liveness отличается от readiness?
5. Как атомарно изменить данные в БД и отправить событие в Kafka?
6. Как гарантируется порядок сообщений в Kafka? Как обрабатывать дубли?
7. gRPC или REST между сервисами? Почему?

## Практика

- [06-system-design](../06-system-design/README.md) — распределённые паттерны и 5 кейсов.
- [CODE_REVIEW.md](../08-interview/CODE_REVIEW.md): №4 «Консьюмер».
- [09-final-project](../09-final-project/README.md) — сервис целиком: слои, outbox, метрики, graceful shutdown, `DESIGN.md`.

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Cache-aside с TTL
// expect: Велосипед Велосипед Велосипед
// expect: loads: 1
// expect: after ttl: Велосипед, loads: 2
// expect: after invalidate: Самокат, loads: 3
// hint: Get: есть запись и c.now() < expires → из кэша; иначе load, сохранить с expires = now + ttl.
// hint: Время — через поле now func() time.Time, а не time.Now(): так TTL проверяется без sleep.
package main

import (
	"fmt"
	"time"
)

type entry struct {
	val     string
	expires time.Time
}

type Cache struct {
	load  func(id int) string
	ttl   time.Duration
	now   func() time.Time
	items map[int]entry
}

func NewCache(load func(int) string, ttl time.Duration, now func() time.Time) *Cache {
	return &Cache{load: load, ttl: ttl, now: now, items: map[int]entry{}}
}

func (c *Cache) Get(id int) string { panic("TODO") }
func (c *Cache) Invalidate(id int) { panic("TODO") }

func main() {
	db := map[int]string{1: "Велосипед"}
	loads := 0
	load := func(id int) string { loads++; return db[id] }

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCache(load, time.Minute, func() time.Time { return clock })

	fmt.Println(c.Get(1), c.Get(1), c.Get(1))
	fmt.Println("loads:", loads)

	clock = clock.Add(2 * time.Minute)
	fmt.Printf("after ttl: %s, loads: %d\n", c.Get(1), loads)

	db[1] = "Самокат" // запись в БД → инвалидация кэша
	c.Invalidate(1)
	fmt.Printf("after invalidate: %s, loads: %d\n", c.Get(1), loads)
}
```

```go
// exercise: Декоратор с метриками
// expect: ad 1: Велосипед
// expect: ad 2: not found
// expect: ad 1: Велосипед
// expect: calls=3 errors=1
// hint: Декоратор реализует тот же интерфейс и держит next Repo. Сервис не знает, что под ним метрики.
// hint: Так же навешивают логирование, трейсинг, кэш и retry — не трогая ни сервис, ни настоящий репозиторий.
package main

import (
	"context"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

type Repo interface {
	Get(ctx context.Context, id int) (string, error)
}

type memRepo map[int]string

func (m memRepo) Get(_ context.Context, id int) (string, error) {
	if v, ok := m[id]; ok {
		return v, nil
	}
	return "", ErrNotFound
}

// metricsRepo считает вызовы и ошибки. TODO: реализуй Repo.
type metricsRepo struct {
	next          Repo
	calls, errors int
}

func main() {
	m := &metricsRepo{next: memRepo{1: "Велосипед"}}
	var repo Repo = m
	for _, id := range []int{1, 2, 1} {
		v, err := repo.Get(context.Background(), id)
		if err != nil {
			fmt.Printf("ad %d: %v\n", id, err)
			continue
		}
		fmt.Printf("ad %d: %s\n", id, v)
	}
	fmt.Printf("calls=%d errors=%d\n", m.calls, m.errors)
}
```

## Самопроверка

- [ ] Нарисую слои сервиса и направление зависимостей, объясню, где объявлены интерфейсы.
- [ ] Напишу `main` с ручной сборкой зависимостей и `run() error`.
- [ ] Объясню outbox, inbox и почему консьюмер должен быть идемпотентным.
- [ ] Назову метрики RED/USE для сервиса продвижения.

Дальше: [Глава 19. Надёжность](19-reliability.md)
