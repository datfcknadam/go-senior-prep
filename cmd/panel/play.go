package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Playground компилирует и запускает произвольные сниппеты из теории и упражнений.
// Сниппет без `package` оборачивается в main, импорты расставляет goimports (если найден).
type Playground struct {
	goVersion string // из go.mod репозитория: от него зависит семантика, например loopvar
	goimports string // путь к goimports или ""
	sem       chan struct{}
}

func newPlayground(root string, parallel int) *Playground {
	p := &Playground{goVersion: modGoVersion(filepath.Join(root, "go.mod")), sem: make(chan struct{}, parallel)}
	if path, err := exec.LookPath("goimports"); err == nil {
		p.goimports = path
	} else if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
		for _, dir := range filepath.SplitList(strings.TrimSpace(string(out))) {
			if path := filepath.Join(dir, "bin", "goimports"); fileExists(path) {
				p.goimports = path
				break
			}
		}
	}
	return p
}

type PlayRequest struct {
	Code     string `json:"code"`
	Mode     string `json:"mode"`               // run | race | escape | test
	Exercise string `json:"exercise,omitempty"` // id упражнения: вывод сверяется с ожидаемым
}

type PlayResult struct {
	Mode      string          `json:"mode"`
	Phase     string          `json:"phase"` // build | run
	Output    string          `json:"output"`
	Stdout    string          `json:"-"`
	ExitCode  int             `json:"exitCode"`
	Timeout   bool            `json:"timeout"`
	Truncated bool            `json:"truncated"`
	Duration  float64         `json:"duration"`
	Source    string          `json:"source,omitempty"` // итоговый код, если сниппет обёрнут или импорты дописаны
	Command   string          `json:"command"`
	Exercise  *ExerciseResult `json:"exercise,omitempty"`
}

type ExerciseResult struct {
	Passed bool   `json:"passed"`
	Expect string `json:"expect"`
	Got    string `json:"got"`
}

const (
	maxPlayCode   = 64 << 10
	maxPlayOutput = 64 << 10
	buildTimeout  = time.Minute
	runTimeout    = 10 * time.Second
	testTimeout   = 30 * time.Second
)

func (p *Playground) Run(ctx context.Context, req PlayRequest) (*PlayResult, error) {
	if len(req.Code) > maxPlayCode {
		return nil, errors.New("код больше 64 КБ")
	}
	switch req.Mode {
	case "run", "race", "escape", "test":
	default:
		return nil, fmt.Errorf("неизвестный режим %q", req.Mode)
	}

	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	dir, err := os.MkdirTemp("", "goplay-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	// На macOS $TMPDIR — симлинк в /private/var: без этого пути в выводе не вычистятся.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}

	wrapped := wrapSnippet(req.Code)
	src := p.fixImports(ctx, wrapped)
	res := &PlayResult{Mode: req.Mode, Phase: "build"}
	// Показываем итоговый код, только если поменялся смысл, а не форматирование:
	// иначе номера строк в ошибках не совпадут с редактором без объяснения.
	if wrapped != req.Code || !slices.Equal(importPaths(req.Code), importPaths(src)) {
		res.Source = src
	}

	file := "main.go"
	if req.Mode == "test" {
		file = "main_test.go"
	}
	gomod := "module play\n\ngo " + p.goVersion + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o644); err != nil {
		return nil, err
	}

	// Собираем и запускаем бинарь отдельно, а не через `go run`: тогда таймаут убивает
	// саму программу, а не только go, который оставил бы её сиротой.
	bin := filepath.Join(dir, "prog")
	args := []string{"build", "-o", bin}
	if req.Mode == "test" {
		args = []string{"test", "-c", "-o", bin}
	}
	if req.Mode == "race" || req.Mode == "test" {
		args = append(args, "-race")
	}
	if req.Mode == "escape" {
		args = append(args, "-gcflags=-m")
	}
	args = append(args, ".")
	res.Command = "go " + strings.Join(args[:len(args)-1], " ")
	res.Command = strings.Replace(res.Command, bin, "prog", 1) + " " + file

	start := time.Now()
	out, code, timedOut, err := execCapture(ctx, buildTimeout, dir, nil, "go", args...)
	if err != nil {
		return nil, err
	}
	res.Output = cleanPaths(out.String(), dir)
	res.Truncated = out.truncated
	res.Timeout = timedOut
	res.ExitCode = code
	if code != 0 || timedOut || req.Mode == "escape" {
		res.Duration = time.Since(start).Seconds()
		return res, nil
	}

	res.Phase = "run"
	runArgs, timeout := []string(nil), runTimeout
	if req.Mode == "test" {
		runArgs, timeout = []string{"-test.v", "-test.bench=.", "-test.benchmem", "-test.benchtime=200ms"}, testTimeout
		res.Command += " && ./prog " + strings.Join(runArgs, " ")
	} else {
		res.Command += " && ./prog"
	}
	var stdout capWriter
	stdout.max = maxPlayOutput
	out, code, timedOut, err = execCapture(ctx, timeout, dir, &stdout, bin, runArgs...)
	if err != nil {
		return nil, err
	}
	res.Output = cleanPaths(out.String(), dir)
	res.Stdout = stdout.String()
	res.Truncated = out.truncated
	res.Timeout = timedOut
	res.ExitCode = code
	res.Duration = time.Since(start).Seconds()
	return res, nil
}

// execCapture запускает команду с таймаутом. Вывод stdout и stderr идёт в один буфер
// в порядке появления; stdout дополнительно копируется в tee, если он задан.
func execCapture(parent context.Context, timeout time.Duration, dir string, tee *capWriter, name string, args ...string) (*capWriter, int, bool, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	out := &capWriter{max: maxPlayOutput}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	cmd.WaitDelay = time.Second
	cmd.Stderr = out
	cmd.Stdout = out
	if tee != nil {
		cmd.Stdout = &teeWriter{out, tee}
	}

	err := cmd.Run()
	if err := parent.Err(); err != nil {
		return nil, 0, false, err // клиент ушёл — результат никому не нужен
	}
	if ctx.Err() != nil {
		return out, -1, true, nil // таймаут программы — штатный результат
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out, 0, false, nil
	case errors.As(err, &exitErr):
		return out, exitErr.ExitCode(), false, nil
	default:
		return nil, 0, false, err
	}
}

// capWriter — потокобезопасный буфер с ограничением размера: exec копирует stdout и stderr
// из разных горутин, а бесконечный цикл с Println не должен съесть память панели.
type capWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	room := w.max - w.buf.Len()
	if len(p) > room {
		w.truncated = true
		p = p[:max(room, 0)]
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

type teeWriter struct{ a, b *capWriter }

func (t *teeWriter) Write(p []byte) (int, error) {
	t.a.Write(p)
	t.b.Write(p)
	return len(p), nil // обрезка — не ошибка: программа не должна падать с EPIPE
}

func cleanPaths(s, dir string) string {
	s = strings.ReplaceAll(s, dir+string(filepath.Separator), "")
	s = strings.ReplaceAll(s, dir, ".")
	// Шапка `# play` от go build — шум.
	return strings.TrimPrefix(s, "# play\n")
}

var (
	packageRe = regexp.MustCompile(`(?m)^package\s+\w+`)
	declRe    = regexp.MustCompile(`^(func|type|import|const|var)\b`)
	mainRe    = regexp.MustCompile(`(?m)^func main\(\)`)
)

// wrapSnippet превращает фрагмент из учебника в программу: объявления верхнего уровня
// (func, type, var, …, начатые с нулевого отступа) остаются снаружи, остальное идёт в main.
func wrapSnippet(code string) string {
	if packageRe.MatchString(code) {
		return code
	}
	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	var decls, body []string
	for i := 0; i < len(lines); i++ {
		if !declRe.MatchString(lines[i]) {
			body = append(body, lines[i])
			continue
		}
		depth := 0
		for ; i < len(lines); i++ {
			decls = append(decls, lines[i])
			depth += braceDelta(lines[i])
			if depth <= 0 {
				break
			}
		}
		decls = append(decls, "")
	}

	var b strings.Builder
	b.WriteString("package main\n\n")
	if len(decls) > 0 {
		b.WriteString(strings.Join(decls, "\n") + "\n")
	}
	if blank(body) {
		if !mainRe.MatchString(code) {
			b.WriteString("func main() {}\n")
		}
		return b.String()
	}
	b.WriteString("func main() {\n")
	for _, l := range body {
		if l != "" {
			b.WriteString("\t")
		}
		b.WriteString(l + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// braceDelta считает баланс скобок в строке, пропуская строки, руны и комментарии.
func braceDelta(line string) int {
	d := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return d
		case c == '{' || c == '(':
			d++
		case c == '}' || c == ')':
			d--
		}
	}
	return d
}

func blank(lines []string) bool {
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "//") {
			return false
		}
	}
	return true
}

// fixImports дописывает импорты. Ошибка goimports (обычно синтаксис) не фатальна:
// компилятор покажет её понятнее.
func (p *Playground) fixImports(ctx context.Context, src string) string {
	if p.goimports == "" {
		return addStdImports(src)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.goimports)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.Output()
	if err != nil {
		return addStdImports(src)
	}
	return string(out)
}

// stdPkgs — запасной вариант без goimports: самые частые пакеты стандартной библиотеки.
var stdPkgs = map[string]string{
	"atomic": "sync/atomic", "bufio": "bufio", "bytes": "bytes", "cmp": "cmp", "context": "context",
	"errors": "errors", "fmt": "fmt", "http": "net/http", "httptest": "net/http/httptest",
	"io": "io", "iter": "iter", "json": "encoding/json", "maps": "maps", "math": "math", "os": "os",
	"rand": "math/rand/v2", "reflect": "reflect", "runtime": "runtime", "slices": "slices", "slog": "log/slog",
	"sort": "sort", "strconv": "strconv", "strings": "strings", "sync": "sync", "testing": "testing",
	"time": "time", "unicode": "unicode", "unsafe": "unsafe", "utf8": "unicode/utf8", "weak": "weak",
}

var pkgUseRe = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Za-z_]`)

func addStdImports(src string) string {
	if strings.Contains(src, "\nimport ") {
		return src
	}
	seen := map[string]bool{}
	var imports []string
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		for _, m := range pkgUseRe.FindAllStringSubmatch(line, -1) {
			if path := stdPkgs[m[1]]; path != "" && !seen[path] {
				seen[path] = true
				imports = append(imports, fmt.Sprintf("\t%q", path))
			}
		}
	}
	if len(imports) == 0 {
		return src
	}
	return strings.Replace(src, "package main\n", "package main\n\nimport (\n"+strings.Join(imports, "\n")+"\n)\n", 1)
}

// importPaths возвращает импорты файла; при синтаксической ошибке — nil.
func importPaths(src string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	paths := make([]string, len(f.Imports))
	for i, imp := range f.Imports {
		paths[i] = imp.Path.Value
	}
	slices.Sort(paths)
	return paths
}

var goLineRe = regexp.MustCompile(`(?m)^go\s+(\S+)`)

func modGoVersion(path string) string {
	b, err := os.ReadFile(path)
	if err == nil {
		if m := goLineRe.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return "1.22" // минимум, где циклы уже с per-iteration переменными
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// normalizeOutput сравнивает вывод без хвостовых пробелов и пустых строк в конце.
func normalizeOutput(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
