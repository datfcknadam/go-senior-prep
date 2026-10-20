# Глава 17. Работа с базой данных из Go

## Зачем

Типичный бэкенд-сервис на Go — это Go + PostgreSQL. Устройство самой базы (индексы, MVCC, изоляция) разобрано в
[05-databases](../05-databases/README.md). Эта глава о стороне приложения: пул соединений, транзакции, контекст, обработка ошибок,
типичные баги на ревью и тестирование.

---

## 1. database/sql и pgx 🟢

- `database/sql` — общий интерфейс к любой SQL-базе через драйвер. `*sql.DB` — **не соединение, а пул**: потокобезопасен, создаётся один раз на процесс.
- `pgx` — драйвер и самостоятельная библиотека для Postgres. Нативный API (`pgxpool`) быстрее, поддерживает типы Postgres, `COPY`, batch, `LISTEN/NOTIFY`.
  В новых сервисах на Postgres обычно берут `pgx/v5` + `pgxpool`.
- Над ними: `sqlc` (генерирует типобезопасный Go-код из SQL — популярный выбор), `squirrel` (построитель запросов), ORM (GORM — реже в высоконагруженных сервисах).

```go
pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
if err != nil {
    return fmt.Errorf("connect db: %w", err)
}
defer pool.Close()

var title string
err = pool.QueryRow(ctx, `SELECT title FROM ads WHERE id = $1`, id).Scan(&title)
```

## 2. Запросы 🟢

```go
rows, err := pool.Query(ctx, `
    SELECT id, title, price FROM ads
    WHERE category_id = $1 AND status = 'active'
    ORDER BY created_at DESC, id DESC
    LIMIT $2`, categoryID, limit)
if err != nil {
    return nil, fmt.Errorf("query ads: %w", err)
}
defer rows.Close()          // ОБЯЗАТЕЛЬНО: иначе соединение не вернётся в пул

var ads []Ad
for rows.Next() {
    var a Ad
    if err := rows.Scan(&a.ID, &a.Title, &a.Price); err != nil {
        return nil, err
    }
    ads = append(ads, a)
}
if err := rows.Err(); err != nil { // ошибка могла случиться посреди чтения
    return nil, err
}
```

С pgx то же короче: `pgx.CollectRows(rows, pgx.RowToStructByName[Ad])`.

- **Только плейсхолдеры** (`$1`, `$2`) — никогда не собирай SQL конкатенацией из пользовательского ввода: SQL-инъекция.
  Имена колонок для сортировки берут из белого списка.
- **Контекст** на каждый запрос: отмена клиента или таймаут прервут запрос в базе.
- NULL: `sql.NullString`, `*string`, `pgtype.Text`. Решай на уровне схемы, где NULL действительно нужен.
- Нет строк в `QueryRow` → `sql.ErrNoRows` / `pgx.ErrNoRows`. На границе репозитория переводи в доменный `ErrNotFound` (глава 6).

## 3. Пул соединений 🟡

```go
cfg, _ := pgxpool.ParseConfig(dsn)
cfg.MaxConns = 20                       // ≤ (max_connections базы − запас) / число подов
cfg.MinConns = 2
cfg.MaxConnLifetime = 30 * time.Minute  // ротация: балансировка после failover, освобождение памяти бэкенда
cfg.MaxConnIdleTime = 5 * time.Minute
```

- В Postgres соединение — отдельный **процесс** (~10 МБ). 50 подов × 50 соединений = 2500 процессов — база ляжет раньше, чем по CPU.
  Отсюда **PgBouncer** в transaction mode. Осторожно: в этом режиме нельзя полагаться на состояние сессии (`SET`, advisory locks на сессию, prepared statements без поддержки).
- **Исчерпание пула** выглядит так: CPU низкий, запросы медленные, горутины висят в ожидании соединения. Диагностика: `pool.Stat()` (`AcquireCount`, `EmptyAcquireCount`, `AcquireDuration`),
  для `database/sql` — `db.Stats().WaitCount`/`WaitDuration`. Экспортируй это в метрики.
- Причины исчерпания: долгие транзакции, незакрытые `rows`, внешние вызовы внутри транзакции, слишком маленький пул.

## 4. Транзакции 🟢

```go
func (r *Repo) Purchase(ctx context.Context, userID, adID int64, price int64) error {
    tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx) // после Commit — безопасный no-op

    // атомарное условное списание: баланс не уйдёт в минус при параллельных покупках
    tag, err := tx.Exec(ctx,
        `UPDATE wallets SET balance = balance - $1 WHERE user_id = $2 AND balance >= $1`, price, userID)
    if err != nil {
        return fmt.Errorf("debit: %w", err)
    }
    if tag.RowsAffected() == 0 {
        return domain.ErrInsufficientFunds
    }
    if _, err := tx.Exec(ctx,
        `INSERT INTO promotions (ad_id, user_id, expires_at) VALUES ($1, $2, now() + interval '24 hours')`,
        adID, userID); err != nil {
        return fmt.Errorf("insert promotion: %w", err)
    }
    return tx.Commit(ctx)
}
```

- **Транзакция привязана к одному соединению.** Все запросы транзакции идут через `tx`, а не через `pool` — иначе они уйдут в другое соединение мимо транзакции.
- Транзакция должна быть **короткой**: никаких HTTP-вызовов, Kafka и ожиданий внутри. Иначе держишь соединение и блокировки, а MVCC копит мёртвые версии.
- **Lost update** (прочитал → посчитал в Go → записал) — классика ревью. Решения: атомарный `UPDATE ... SET x = x - $1 WHERE ...`,
  `SELECT ... FOR UPDATE`, оптимистическая блокировка через колонку `version`.
- На уровнях Repeatable Read и Serializable приложение **обязано ретраить** транзакцию при `40001 serialization_failure` (и при `40P01 deadlock_detected`):
  ```go
  var pgErr *pgconn.PgError
  if errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01") {
      // повторить всю транзакцию с backoff — глава 19
  }
  ```
- Где хранить транзакцию в коде, чтобы сервисный слой мог объединять вызовы нескольких репозиториев — разобрано в главе 18 (паттерн «transactor»).

## 5. Производительность запросов из приложения 🟡

- **N+1** — цикл с запросом на каждый элемент. Лечится одним запросом `WHERE id = ANY($1)` с массивом id и сборкой в мапу.
- **Батчинг записи:** `pgx.Batch` (несколько запросов за один round-trip) или `COPY` (`pool.CopyFrom`) для массовой вставки.
- **Пагинация:** `OFFSET 100000` читает и выбрасывает 100 000 строк. Keyset-пагинация (`WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT 50`)
  работает за константу по индексу. Курсор для клиента — закодированная пара последних значений.
- `SELECT *` в коде — хрупко (новая колонка сломает `Scan`) и дорого.
- Таймаут запроса на стороне базы: `statement_timeout` на роль или сессию, плюс контекст в Go.
- Чтение с реплик: учитывай replication lag и read-your-writes (подробно в 05-databases).

## 6. Миграции 🟡

- Инструменты: `goose`, `golang-migrate`, `atlas`. Миграции — файлы в репозитории, применяются отдельным шагом деплоя, а не при старте каждого пода.
- **Обратная совместимость:** во время выката работают старая и новая версии кода одновременно. Схема меняется в три шага **expand → migrate → contract**:
  добавить новую колонку (nullable) → писать в обе, перенести данные батчами → переключить чтение → удалить старую.
- `ALTER TABLE` берёт `ACCESS EXCLUSIVE` и встаёт в очередь, блокируя за собой все запросы. На проде: `SET lock_timeout = '2s'` и ретраи миграции.
- Индексы — только `CREATE INDEX CONCURRENTLY` (не внутри транзакции).

## 7. Тестирование 🟡

- Репозитории тестируют на **настоящем Postgres**: `testcontainers-go` поднимает контейнер, миграции применяются перед тестами.
- Изоляция: транзакция на тест с откатом, отдельная схема или `TRUNCATE` между тестами.
- Конкурентные сценарии (100 параллельных покупок) проверяют именно на реальной базе: моки не покажут lost update.
- Сервисный слой тестируют с fake-репозиторием (глава 9).

## Под капотом 🔴

- `database/sql` держит список свободных соединений и очередь ожидающих запросов. `Query` берёт соединение и **не отдаёт** до `rows.Close()`.
  Поэтому незакрытый `rows` — утечка соединения, а не только памяти.
- Протокол Postgres: простой запрос (текст целиком) и расширенный (Parse/Bind/Execute). pgx по умолчанию кеширует prepared statements на соединении,
  поэтому с PgBouncer в transaction mode нужен режим `QueryExecModeExec` или поддержка prepared statements на стороне PgBouncer (1.21+).
- Отмена по контексту в pgx — отдельное соединение с `CancelRequest` к серверу, а затем закрытие текущего соединения, если отмена не успела.

## Ловушки

- Незакрытый `rows`, непроверенный `rows.Err()`.
- Запросы через `pool` внутри транзакции вместо `tx`.
- HTTP-вызов внутри транзакции.
- Read-modify-write без блокировки или атомарного `UPDATE`.
- `fmt.Sprintf` для SQL с пользовательскими данными.
- N+1 и `OFFSET`-пагинация на больших таблицах.
- Миграция, несовместимая с работающей версией кода.

## Спросят

1. Как гарантировать, что баланс не уйдёт в минус при 100 параллельных покупках? Назови три способа.
2. Что такое lost update и write skew? На каком уровне изоляции что лечится?
3. Как подобрать размер пула соединений? Что будет при 100 подах?
4. Почему нельзя делать внешний вызов внутри транзакции?
5. Как выкатить переименование колонки без даунтайма?
6. Как ты тестируешь репозитории?

## Практика

- [05-databases/TASKS.md](../05-databases/TASKS.md): №1 «Лента категории» (keyset), №5 «Lost update», №6 «Очередь задач в Postgres» (`SKIP LOCKED`), №8 «Миграция без даунтайма».
- [CODE_REVIEW.md](../08-interview/CODE_REVIEW.md): №1 «Загрузка объявлений» — ищи N+1 и незакрытые ресурсы.
- [09-final-project](../09-final-project/README.md): кошелёк с ledger и тестом на 100 параллельных покупок.

## Закрепление

Упражнения запускаются прямо в панели (`make panel`, эта глава): правь код в редакторе и жми ▶ или <kbd>⌘/Ctrl+Enter</kbd>. Упражнение засчитывается, когда вывод совпал с ожидаемым; конкурентные прогоняются с `-race`. Подсказки открываются по одной — сначала попробуй без них.

```go
// exercise: Транзакция-обёртка
// expect: begin
// expect: exec insert ad
// expect: commit
// expect: err: <nil>
// expect: begin
// expect: rollback
// expect: err: insert: duplicate key
// expect: begin
// expect: rollback
// expect: panic: oops
// hint: Именованный результат err + defer: при панике — Rollback и panic(p) дальше; при ошибке — Rollback; иначе err = tx.Commit().
// hint: Паника не должна оставить транзакцию открытой: соединение вернётся в пул с висящей транзакцией и блокировками.
package main

import (
	"errors"
	"fmt"
)

type Tx interface {
	Exec(query string) error
	Commit() error
	Rollback() error
}

type DB interface{ Begin() (Tx, error) }

type fakeDB struct{}
type fakeTx struct{}

func (fakeDB) Begin() (Tx, error)  { fmt.Println("begin"); return fakeTx{}, nil }
func (fakeTx) Exec(q string) error { fmt.Println("exec", q); return nil }
func (fakeTx) Commit() error       { fmt.Println("commit"); return nil }
func (fakeTx) Rollback() error     { fmt.Println("rollback"); return nil }

var errDuplicate = errors.New("duplicate key")

// WithTx выполняет fn в транзакции: commit при успехе, rollback при ошибке или панике.
func WithTx(db DB, fn func(Tx) error) (err error) {
	panic("TODO")
}

func main() {
	var db fakeDB
	err := WithTx(db, func(tx Tx) error { return tx.Exec("insert ad") })
	fmt.Println("err:", err)

	err = WithTx(db, func(tx Tx) error { return fmt.Errorf("insert: %w", errDuplicate) })
	fmt.Println("err:", err)

	func() {
		defer func() { fmt.Println("panic:", recover()) }()
		WithTx(db, func(tx Tx) error { panic("oops") })
	}()
}
```

```go
// exercise: Keyset-пагинация
// expect: page 1: [9 8 7]
// expect: page 2: [6 5 4]
// expect: page 3: [3 2 1]
// hint: Курсор — последняя пара (CreatedAt, ID) страницы. «После курсора» в порядке DESC значит: CreatedAt < c.CreatedAt или (равны и ID < c.ID).
// hint: Курсор только по времени теряет записи с одинаковым CreatedAt на границе страниц — поэтому ID в курсоре обязателен.
package main

import (
	"fmt"
	"time"
)

type Ad struct {
	ID        int
	CreatedAt time.Time
}

type Cursor struct {
	CreatedAt time.Time
	ID        int
}

// Page — аналог WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3.
// after == nil — первая страница. Следующий курсор nil, если страница неполная.
func Page(ads []Ad, after *Cursor, limit int) ([]Ad, *Cursor) {
	panic("TODO")
}

func main() {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	ads := []Ad{ // время совпадает у нескольких объявлений — как в реальной ленте
		{5, at(20)}, {1, at(10)}, {9, at(40)}, {3, at(20)}, {7, at(30)},
		{2, at(10)}, {8, at(40)}, {4, at(20)}, {6, at(30)},
	}
	var cur *Cursor
	for n := 1; ; n++ {
		page, next := Page(ads, cur, 3)
		if len(page) == 0 {
			break
		}
		ids := make([]int, len(page))
		for i, a := range page {
			ids[i] = a.ID
		}
		fmt.Printf("page %d: %v\n", n, ids)
		if next == nil {
			break
		}
		cur = next
	}
}
```

## Самопроверка

- [ ] Напишу репозиторий на pgx с транзакцией, атомарным списанием и переводом ошибок в доменные.
- [ ] Объясню, как незакрытый `rows` приводит к деградации всего сервиса.
- [ ] Распишу expand → migrate → contract на примере.

Дальше: [Глава 18. Архитектура сервиса](18-architecture.md)
