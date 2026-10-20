# Задача: retry с exponential backoff и jitter

```go
err := retry.Do(ctx, retry.Policy{
    Attempts:  5,
    BaseDelay: 100 * time.Millisecond,
    MaxDelay:  2 * time.Second,
}, func(ctx context.Context) error {
    return client.Call(ctx)
})
```

- Не больше `Attempts` вызовов `fn` (включая первый).
- Задержка перед попыткой `n` (n = 1, 2, ...): **full jitter** — случайное значение в `[0, min(MaxDelay, BaseDelay·2^(n-1))]`.
- Ошибка, обёрнутая в `Permanent(err)`, повторять не нужно — сразу вернуть исходную `err` (без обёртки, `errors.Is` должен работать).
- Отмена `ctx` во время ожидания — сразу вернуть `ctx.Err()`, без лишних вызовов.
- Все попытки исчерпаны — вернуть последнюю ошибку.
- Внутренняя функция `sleep` подменяется в тестах (`Policy.Sleep`); если `nil`, ждать по-настоящему через таймер и `select`.

Вопросы:

- Почему full jitter, а не просто экспонента? (статья AWS «Exponential Backoff And Jitter»)
- Сколько суммарно будет ждать пользователь при 5 попытках? Согласуй это с таймаутом запроса.
- Как не устроить retry storm? (retry budget, ретраи на одном уровне, circuit breaker)

```bash
go test -race -v ./04-patterns/retry
```
