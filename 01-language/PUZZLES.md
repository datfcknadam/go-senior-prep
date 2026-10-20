# Головоломки «что выведет?»

Отвечай вслух и объясняй **почему**. На собеседовании важен ход мысли, а не угаданный ответ.
Проверить можно в `go run` или на go.dev/play.

---

### 1. Append и общий массив

```go
a := make([]int, 3, 10)
b := append(a, 1)
c := append(a, 2)
fmt.Println(b[3], c[3])
```

<details><summary>Ответ</summary>

`2 2`. `cap(a) = 10`, поэтому оба `append` пишут в один и тот же массив, в индекс 3. Второй перезаписывает первый.
</details>

---

### 2. Слайс в функции

```go
func add(s []int) { s = append(s, 4); s[0] = 100 }

s := []int{1, 2, 3}
add(s)
fmt.Println(s)
```

<details><summary>Ответ</summary>

`[1 2 3]`. `cap(s) == 3`, поэтому `append` аллоцирует новый массив, и `s[0] = 100` пишет уже в него.
Если бы был `s := make([]int, 3, 4)`, результат был бы `[100 0 0]`, а 4 не видна из-за len.
</details>

---

### 3. Nil-интерфейс

```go
type MyErr struct{}
func (*MyErr) Error() string { return "boom" }

func do() error {
    var e *MyErr
    return e
}

fmt.Println(do() == nil)
```

<details><summary>Ответ</summary>

`false`. В интерфейсе лежит пара (тип `*MyErr`, значение `nil`). Интерфейс равен nil, только когда пусты обе части.
</details>

---

### 4. defer и аргументы

```go
x := 1
defer fmt.Println(x)
x = 2
```

<details><summary>Ответ</summary>

`1`. Аргументы отложенного вызова вычисляются в момент `defer`. С `defer func() { fmt.Println(x) }()` было бы `2`.
</details>

---

### 5. defer и именованный результат

```go
func f() (n int) {
    defer func() { n *= 2 }()
    return 3
}
```

<details><summary>Ответ</summary>

`6`. `return 3` сначала присваивает `n = 3`, потом выполняются defer-ы.
</details>

---

### 6. Замыкания в цикле

```go
var fns []func()
for i := 0; i < 3; i++ {
    fns = append(fns, func() { fmt.Print(i) })
}
for _, f := range fns { f() }
```

<details><summary>Ответ</summary>

С Go 1.22 — `012`: у каждой итерации своя переменная `i`. До 1.22 было `333`.
Это любят спрашивать: «что изменилось в 1.22 и почему».
</details>

---

### 7. Мапа структур

```go
type P struct{ X int }
m := map[string]P{"a": {1}}
m["a"].X = 2
```

<details><summary>Ответ</summary>

Ошибка компиляции: `cannot assign to struct field m["a"].X in map`. Элемент мапы не адресуем.
Нужно `p := m["a"]; p.X = 2; m["a"] = p` или `map[string]*P`.
</details>

---

### 8. Range по слайсу и изменения

```go
s := []int{1, 2, 3}
for i, v := range s {
    if i == 0 { s = append(s, 4); s[1] = 20 }
    fmt.Print(v, " ")
}
```

<details><summary>Ответ</summary>

`1 2 3`. `range` один раз вычисляет исходный слайс (len 3, старый массив). `append` при cap 3 создаёт новый массив,
и `s[1] = 20` пишет уже в него — `range` его не видит.
</details>

---

### 9. Закрытый канал

```go
ch := make(chan int, 2)
ch <- 1
close(ch)
v1, ok1 := <-ch
v2, ok2 := <-ch
fmt.Println(v1, ok1, v2, ok2)
```

<details><summary>Ответ</summary>

`1 true 0 false`. Из закрытого канала сначала вычитывается буфер, потом нулевое значение и `ok == false`.
Запись в закрытый канал → panic. Повторный `close` → panic. Чтение и запись в nil-канал блокируются навсегда.
</details>

---

### 10. select без готовых каналов

```go
var ch chan int
select {
case v := <-ch:
    fmt.Println(v)
default:
    fmt.Println("default")
}
```

<details><summary>Ответ</summary>

`default`. Nil-канал никогда не готов. Приём: присвоить каналу `nil` внутри `select`, чтобы «выключить» case.
</details>

---

### 11. Горутины и WaitGroup

```go
var wg sync.WaitGroup
for i := 0; i < 3; i++ {
    go func() {
        wg.Add(1)
        defer wg.Done()
        fmt.Print(i)
    }()
}
wg.Wait()
```

<details><summary>Ответ</summary>

Гонка: `wg.Add` вызывается внутри горутины, и `Wait` может отработать раньше, чем хоть одна горутина стартует.
Выведется что угодно, от пустоты до `012` в любом порядке. `Add` должен быть до `go`, или `wg.Go(func(){...})` (Go 1.25+).
</details>

---

### 12. Recover

```go
func main() {
    defer func() {
        fmt.Println(recover())
    }()
    go func() { panic("boom") }()
    time.Sleep(time.Second)
}
```

<details><summary>Ответ</summary>

Процесс падает с `panic: boom`. `recover` ловит только панику своей горутины.
</details>

---

### 13. Копирование мьютекса

```go
type Counter struct {
    mu sync.Mutex
    n  int
}
func (c Counter) Inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
```

<details><summary>Ответ</summary>

Ресивер по значению: каждый вызов работает с копией, и счётчик не растёт. Плюс копируется мьютекс, на что ругается `go vet` (copylocks).
Нужен ресивер `*Counter`.
</details>

---

### 14. String и байты

```go
s := "привет"
fmt.Println(len(s), len([]rune(s)), s[0])
```

<details><summary>Ответ</summary>

`12 6 208`. `len` считает байты UTF-8 (кириллица — 2 байта), `s[0]` — первый байт, а не символ.
`for _, r := range s` итерирует по рунам.
</details>

---

### 15. Сравнение интерфейсов

```go
var a any = []int{1}
var b any = []int{1}
fmt.Println(a == b)
```

<details><summary>Ответ</summary>

Panic в рантайме: `comparing uncomparable type []int`. Компилятор это пропускает, потому что статический тип `any`.
Такая же паника будет, если положить слайс как ключ в `map[any]T`.
</details>
