# SQL-задачи

Схема — в [`schema.sql`](schema.sql). Для каждой задачи: напиши запрос, посмотри `EXPLAIN (ANALYZE, BUFFERS)`,
подумай, какой индекс нужен, создай его и сравни планы.

---

### 1. Лента категории

Последние 20 активных объявлений в категории 5, отсортированные по `created_at DESC`. Затем — следующая страница.

<details><summary>Разбор</summary>

```sql
SELECT id, title, price, created_at
FROM ads
WHERE category_id = 5 AND status = 'active'
ORDER BY created_at DESC, id DESC
LIMIT 20;

CREATE INDEX CONCURRENTLY ads_feed_idx
    ON ads (category_id, created_at DESC, id DESC) WHERE status = 'active';
```

Следующая страница — **keyset pagination**, а не `OFFSET`:

```sql
WHERE category_id = 5 AND status = 'active'
  AND (created_at, id) < ($last_created_at, $last_id)
ORDER BY created_at DESC, id DESC
LIMIT 20;
```

`OFFSET 10000` читает и выбрасывает 10 000 строк, а keyset всегда стоит одинаково. `id` в сортировке нужен для однозначности.
</details>

---

### 2. Топ продавцов

Топ-10 пользователей по числу активных объявлений в каждом городе.

<details><summary>Разбор</summary>

```sql
SELECT city, user_id, cnt
FROM (
    SELECT u.city, a.user_id, count(*) AS cnt,
           row_number() OVER (PARTITION BY u.city ORDER BY count(*) DESC) AS rn
    FROM ads a
    JOIN users u ON u.id = a.user_id
    WHERE a.status = 'active'
    GROUP BY u.city, a.user_id
) t
WHERE rn <= 10
ORDER BY city, cnt DESC;
```

Обсуди: `row_number` vs `rank` vs `dense_rank` при равенстве; для дашборда в проде такое считают заранее (материализованное представление / ClickHouse).
</details>

---

### 3. Дерево категорий

Все объявления категории «Транспорт» (id = 1) включая подкатегории любой глубины.

<details><summary>Разбор</summary>

```sql
WITH RECURSIVE tree AS (
    SELECT id FROM categories WHERE id = 1
    UNION ALL
    SELECT c.id FROM categories c JOIN tree t ON c.parent_id = t.id
)
SELECT count(*) FROM ads WHERE category_id IN (SELECT id FROM tree);
```

Альтернативы: materialized path (`ltree`), nested sets, таблица замыканий — компромисс между скоростью чтения и стоимостью изменения дерева.
</details>

---

### 4. Дубликаты

Найди пользователей, у которых есть несколько активных объявлений с одинаковым заголовком, и удали дубли, оставив самое раннее.

<details><summary>Разбор</summary>

```sql
DELETE FROM ads a
USING (
    SELECT id, row_number() OVER (PARTITION BY user_id, title ORDER BY created_at, id) AS rn
    FROM ads
    WHERE status = 'active'
) d
WHERE a.id = d.id AND d.rn > 1;
```

На проде удаление миллионов строк — батчами (`LIMIT` + цикл), иначе одна огромная транзакция, блокировки, лаг репликации и bloat.
Защита от повторения — уникальный индекс: `CREATE UNIQUE INDEX ... ON ads (user_id, title) WHERE status = 'active'`.
</details>

---

### 5. Lost update

Два модератора одновременно меняют цену объявления: читают объявление, правят в UI, сохраняют. Как не потерять чужое изменение?

<details><summary>Разбор</summary>

Оптимистическая блокировка:

```sql
UPDATE ads SET price = $1, version = version + 1
WHERE id = $2 AND version = $3;
-- 0 строк обновлено → кто-то успел раньше → 409 Conflict, перечитать и показать пользователю
```

Пессимистическая — `SELECT ... FOR UPDATE` в транзакции; подходит, когда между чтением и записью нет человека.
Держать транзакцию открытой, пока пользователь думает, нельзя.
</details>

---

### 6. Очередь задач в Postgres

Таблица `jobs(id, payload, status, locked_until)`. Несколько воркеров должны забирать задачи без дублей и без ожидания друг друга.

<details><summary>Разбор</summary>

```sql
UPDATE jobs SET status = 'processing', locked_until = now() + interval '5 minutes'
WHERE id IN (
    SELECT id FROM jobs
    WHERE status = 'pending' OR (status = 'processing' AND locked_until < now())
    ORDER BY id
    LIMIT 10
    FOR UPDATE SKIP LOCKED
)
RETURNING id, payload;
```

`SKIP LOCKED` — воркеры не ждут чужие блокировки. `locked_until` — возврат задач от упавших воркеров (visibility timeout).
Обсуди: когда пора уходить с этого подхода на Kafka/RabbitMQ.
</details>

---

### 7. Статистика просмотров

Количество просмотров и уникальных зрителей по дням за последние 7 дней для объявления 42.
Почему такой запрос по `ad_views` на сотнях миллионов строк — плохая идея для OLTP-базы, и как это делают в проде?

<details><summary>Разбор</summary>

```sql
SELECT date_trunc('day', viewed_at) AS day, count(*), count(DISTINCT viewer_id)
FROM ad_views
WHERE ad_id = 42 AND viewed_at >= now() - interval '7 days'
GROUP BY 1 ORDER BY 1;
-- индекс (ad_id, viewed_at)
```

В проде: события просмотров → Kafka → ClickHouse (колоночная, агрегаты), либо предагрегированные счётчики (Redis / таблица `ad_daily_stats`).
Уникальные — приближённо через HyperLogLog. Таблицу событий — партиционировать по времени.
</details>

---

### 8. Миграция без даунтайма

Нужно переименовать колонку `title` → `name` в таблице на 500 млн строк, сервис работает в 50 подах.

<details><summary>Разбор</summary>

Expand / contract:

1. Добавить `name` (nullable, без дефолта — мгновенно).
2. Код пишет в обе колонки.
3. Бэкфилл батчами.
4. Код читает из `name`.
5. Код перестаёт писать `title`.
6. Удалить `title`.

Каждый DDL — с `lock_timeout`. Каждый шаг совместим с предыдущей версией кода, потому что при раскатке одновременно работают старые и новые поды.
</details>
