# Задача: оптимизация JoinIDs

Сервис собирает SQL `WHERE id IN (...)` / ключ кэша из списка id объявлений. Функция в горячем пути и даёт много аллокаций.

В `task.go` лежит **рабочая, но медленная** версия. Твоя цель:

1. Запусти бенчмарк и запомни цифры:
   ```bash
   go test -run '^$' -bench . -benchmem -count=10 ./03-runtime/joinids | tee old.txt
   ```
2. Посмотри, что убегает в кучу: `go build -gcflags='-m' ./03-runtime/joinids`.
3. Сними профиль памяти: `go test -run '^$' -bench . -memprofile mem.out ./03-runtime/joinids && go tool pprof -http=:8081 mem.out`.
4. Перепиши `JoinIDs` так, чтобы получилась **не больше 1 аллокации** на вызов (тест `TestAllocs` это проверяет).
5. Сравни: `go install golang.org/x/perf/cmd/benchstat@latest && benchstat old.txt new.txt`.

Подсказки по нарастающей:

<details><summary>1</summary>Каждый `+=` для строки — новая аллокация и копирование всего, что было раньше. Сложность O(n²).</details>
<details><summary>2</summary>`strings.Builder` + `Grow`. Сколько байт займёт результат — можно посчитать заранее.</details>
<details><summary>3</summary>`strconv.Itoa` аллоцирует строку. `strconv.AppendInt` пишет в `[]byte`, а массив `[20]byte` на стеке в кучу не убегает.</details>
