# Найди проблемы в коде

Формат, который часто встречается на техническом интервью: дают кусок кода, нужно найти все проблемы и предложить исправления.
Ищи по категориям: корректность → конкурентность → ресурсы → ошибки → производительность → дизайн.

---

### 1. Загрузка объявлений

```go
func LoadAds(ids []int64) map[int64]*Ad {
    result := make(map[int64]*Ad)
    for _, id := range ids {
        go func() {
            ad, err := fetchAd(id)
            if err != nil {
                log.Println(err)
            }
            result[id] = ad
        }()
    }
    time.Sleep(time.Second)
    return result
}
```

<details><summary>Проблемы</summary>

1. Конкурентная запись в map → `fatal error: concurrent map writes`.
2. `time.Sleep` вместо синхронизации: либо возвращаем неполный результат, либо ждём лишнее.
3. Горутины продолжают писать в map после возврата — гонка с вызывающим кодом.
4. Нет лимита параллельности: 100 тысяч id → 100 тысяч одновременных запросов к зависимости.
5. Нет контекста — нельзя отменить, нет таймаута.
6. При ошибке в map кладётся `nil`, ошибка теряется для вызывающего.
7. Нет батчинга: если API зависимости умеет `GetByIDs` — один запрос лучше N.

Исправление: `errgroup.WithContext` + `SetLimit` + мьютекс (или слайс по индексу), возврат ошибки, батчи.
</details>

---

### 2. Кэш

```go
type Cache struct {
    mu   sync.RWMutex
    data map[string]string
}

func (c Cache) Get(k string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.data[k]
}

func (c *Cache) GetOrLoad(k string, load func() string) string {
    c.mu.RLock()
    v, ok := c.data[k]
    c.mu.RUnlock()
    if !ok {
        c.mu.Lock()
        v = load()
        c.data[k] = v
        c.mu.Unlock()
    }
    return v
}
```

<details><summary>Проблемы</summary>

1. `Get` с value receiver копирует мьютекс — блокировка не защищает ничего (go vet: copylocks).
2. `GetOrLoad`: между `RUnlock` и `Lock` другие горутины тоже не нашли ключ → `load` выполнится многократно (stampede). Нужна повторная проверка после `Lock` или singleflight.
3. `load()` под эксклюзивной блокировкой — медленная загрузка одного ключа блокирует весь кэш.
4. Нет TTL и ограничения размера — неограниченный рост памяти.
5. `load` не может вернуть ошибку; нет контекста.
6. Не различаются «нет ключа» и «пустое значение» в `Get`.
</details>

---

### 3. HTTP-обработчик

```go
var db *sql.DB

func handler(w http.ResponseWriter, r *http.Request) {
    id := r.URL.Query().Get("id")
    rows, err := db.Query("SELECT title, price FROM ads WHERE user_id = " + id)
    if err != nil {
        w.WriteHeader(500)
        w.Write([]byte(err.Error()))
    }
    var ads []Ad
    for rows.Next() {
        var a Ad
        rows.Scan(&a.Title, &a.Price)
        ads = append(ads, a)
    }
    json.NewEncoder(w).Encode(ads)
}
```

<details><summary>Проблемы</summary>

1. **SQL-инъекция** — конкатенация строки. Нужны параметры `$1`.
2. После ошибки нет `return` → `rows` равен nil → panic.
3. Текст внутренней ошибки уходит клиенту — утечка информации.
4. Нет `defer rows.Close()` → утечка соединений из пула.
5. Не проверяются ошибки `rows.Scan` и `rows.Err()`.
6. `db.Query` без контекста — запрос продолжится, даже если клиент ушёл. Нужен `QueryContext(r.Context(), ...)`.
7. Нет валидации `id`, нет пагинации/`LIMIT` — можно выгрузить миллион строк.
8. Нет `Content-Type: application/json`; `nil`-слайс кодируется как `null`.
9. Глобальная переменная `db` — плохо тестируется.
</details>

---

### 4. Консьюмер

```go
func consume(ctx context.Context, msgs <-chan Message) {
    for msg := range msgs {
        go func(m Message) {
            if err := process(m); err != nil {
                return
            }
            m.Commit()
        }(msg)
    }
}
```

<details><summary>Проблемы</summary>

1. Неограниченное число горутин — нет backpressure.
2. Коммит оффсетов из параллельных горутин вне порядка: закоммитили сообщение 10, упали на 9 → сообщение 9 потеряно (Kafka коммитит позицию, а не отдельные сообщения).
3. Ошибка молча игнорируется — нет ретрая, DLQ, лога, метрики.
4. `ctx` не используется: нет отмены и graceful shutdown, горутины не ожидаются.
5. Порядок обработки сообщений с одним ключом нарушается — может быть важно для бизнеса (статусы объявления).

Решение: воркеры по хешу ключа (порядок в рамках ключа), коммит только непрерывного префикса обработанных оффсетов, ретраи + DLQ, ожидание при остановке.
</details>
