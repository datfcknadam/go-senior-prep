package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Course — структура курса, собранная по файлам репозитория при старте.
type Course struct {
	Sections []Section `json:"sections"`

	exercises map[string]Exercise // id → упражнение; ожидаемый вывод сверяет сервер
}

// Exercise — блок ```go в markdown, начинающийся с директив:
//
//	// exercise: Название
//	// expect: строка ожидаемого вывода (повторяется построчно)
//	// hint: подсказка (повторяется, открываются по одной)
//	// flags: race
type Exercise struct {
	ID     string
	Expect string
	Race   bool
}

type Section struct {
	Dir    string  `json:"dir"`
	Title  string  `json:"title"`
	Readme string  `json:"readme"`
	Docs   []Doc   `json:"docs"`
	Tasks  []Task  `json:"tasks"`
	Checks []Check `json:"checks"`
}

type Doc struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type Task struct {
	Path  string   `json:"path"` // относительно корня, например 02-concurrency/merge
	Title string   `json:"title"`
	Tests []string `json:"tests"`
	Files []string `json:"files"` // .go-файлы задачи без эталона
}

// Check — пункт чек-листа, который отмечается вручную.
type Check struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Doc   string `json:"doc,omitempty"`  // документ, где пункт можно отметить прямо у заголовка
	Kind  string `json:"kind,omitempty"` // "exercise" — отмечается автоматически, когда вывод совпал
}

// checkDocs — документы, в которых каждый заголовок ### становится пунктом чек-листа.
var checkDocs = map[string][]string{
	"01-language":      {"PUZZLES.md"},
	"05-databases":     {"TASKS.md"},
	"06-system-design": {"README.md"},
	"08-interview":     {"CODE_REVIEW.md"},
}

// extraChecks — пункты, которых нет в виде заголовков.
var extraChecks = map[string][]string{
	"03-runtime": {
		"Разобрал вывод -gcflags=-m для joinids",
		"Снял и прочитал профиль памяти pprof",
		"Сравнил бенчмарки через benchstat",
		"Готова история «нашёл и починил проблему с производительностью»",
	},
	"08-interview": {
		"Ответил вслух на весь банк вопросов",
		"Подготовил 6 историй по STAR",
		"Mock: техническое интервью",
		"Mock: system design",
		"Mock: поведенческое интервью",
	},
	"09-final-project": {
		"Кошелёк: пополнение, баланс, история",
		"Покупка поднятия с Idempotency-Key",
		"Тест на 100 параллельных покупок",
		"Выдача категории с keyset-пагинацией",
		"Outbox + публикация в Kafka",
		"Автозавершение продвижений",
		"Метрики, логи, graceful shutdown",
		"Нагрузочный тест и профиль",
		"DESIGN.md",
	},
}

var (
	sectionDirRe = regexp.MustCompile(`^\d\d-`)
	testFuncRe   = regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)
)

func scanCourse(root string) (*Course, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	c := &Course{exercises: map[string]Exercise{}}
	for _, e := range entries {
		if !e.IsDir() || !sectionDirRe.MatchString(e.Name()) {
			continue
		}
		s, err := scanSection(root, e.Name())
		if err != nil {
			return nil, err
		}
		for _, doc := range append([]string{s.Readme}, docPaths(s.Docs)...) {
			for _, ex := range mdExercises(filepath.Join(root, doc), doc) {
				c.exercises[ex.ID] = ex
				s.Checks = append(s.Checks, Check{ID: ex.ID, Title: strings.TrimPrefix(ex.ID, doc+"#ex:"), Doc: doc, Kind: "exercise"})
			}
		}
		c.Sections = append(c.Sections, s)
	}
	return c, nil
}

func scanSection(root, dir string) (Section, error) {
	// Пустые слайсы, а не nil: в JSON должно быть [], а не null.
	s := Section{Dir: dir, Title: dir, Readme: dir + "/README.md", Docs: []Doc{}, Tasks: []Task{}, Checks: []Check{}}
	if t := mdTitle(filepath.Join(root, s.Readme)); t != "" {
		s.Title = t
	}

	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return s, err
	}
	for _, e := range entries {
		rel := dir + "/" + e.Name()
		switch {
		case e.IsDir():
			if _, err := os.Stat(filepath.Join(root, rel, "task.go")); err != nil {
				continue
			}
			t := Task{Path: rel, Title: e.Name()}
			if title := mdTitle(filepath.Join(root, rel, "README.md")); title != "" {
				t.Title = capitalize(strings.TrimPrefix(title, "Задача: "))
			}
			t.Tests = testNames(filepath.Join(root, rel))
			t.Files = goFiles(filepath.Join(root, rel))
			s.Tasks = append(s.Tasks, t)
		case strings.HasSuffix(e.Name(), ".md") && e.Name() != "README.md":
			title := mdTitle(filepath.Join(root, rel))
			if title == "" {
				title = e.Name()
			}
			s.Docs = append(s.Docs, Doc{Path: rel, Title: title})
		}
	}

	for _, doc := range checkDocs[dir] {
		rel := dir + "/" + doc
		for _, h := range mdHeadings(filepath.Join(root, rel), "### ") {
			s.Checks = append(s.Checks, Check{ID: rel + "#" + h, Title: h, Doc: rel})
		}
	}
	for _, title := range extraChecks[dir] {
		s.Checks = append(s.Checks, Check{ID: dir + "#" + title, Title: title})
	}
	return s, nil
}

func mdTitle(path string) string {
	h := mdHeadings(path, "# ")
	if len(h) == 0 {
		return ""
	}
	// «01. Язык» → «Язык»: номер и так виден в навигации.
	t := h[0]
	if i := strings.Index(t, ". "); i > 0 && i <= 3 {
		t = t[i+2:]
	}
	return t
}

// mdHeadings возвращает заголовки с заданным префиксом, пропуская блоки кода.
func mdHeadings(path, prefix string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	inCode := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if !inCode && strings.HasPrefix(line, prefix) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		}
	}
	return out
}

func docPaths(docs []Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Path
	}
	return out
}

// mdExercises находит упражнения в документе. id = путь#ex:название.
func mdExercises(path, rel string) []Exercise {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Exercise
	var block []string
	inCode, isGo := false, false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case !inCode && strings.HasPrefix(line, "```"):
			inCode, isGo, block = true, strings.TrimSpace(line) == "```go", nil
		case inCode && strings.HasPrefix(line, "```"):
			inCode = false
			if ex, ok := parseExercise(block, rel); ok && isGo {
				out = append(out, ex)
			}
		case inCode:
			block = append(block, line)
		}
	}
	return out
}

func parseExercise(lines []string, doc string) (Exercise, bool) {
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "// exercise:") {
		return Exercise{}, false
	}
	ex := Exercise{ID: doc + "#ex:" + strings.TrimSpace(strings.TrimPrefix(lines[0], "// exercise:"))}
	var expect []string
	for _, l := range lines[1:] {
		switch {
		case strings.HasPrefix(l, "// expect:"):
			expect = append(expect, strings.TrimPrefix(strings.TrimPrefix(l, "// expect:"), " "))
		case strings.HasPrefix(l, "// flags:"):
			ex.Race = ex.Race || strings.Contains(l, "race")
		case strings.HasPrefix(l, "// hint:"):
		default:
			ex.Expect = normalizeOutput(strings.Join(expect, "\n"))
			return ex, true
		}
	}
	ex.Expect = normalizeOutput(strings.Join(expect, "\n"))
	return ex, true
}

func (c *Course) exercise(id string) (Exercise, bool) {
	ex, ok := c.exercises[id]
	return ex, ok
}

func testNames(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	names := []string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range testFuncRe.FindAllSubmatch(b, -1) {
			names = append(names, string(m[1]))
		}
	}
	return names
}

func (c *Course) task(path string) (Task, bool) {
	for _, s := range c.Sections {
		if i := slices.IndexFunc(s.Tasks, func(t Task) bool { return t.Path == path }); i >= 0 {
			return s.Tasks[i], true
		}
	}
	return Task{}, false
}

func (c *Course) hasCheck(id string) bool {
	for _, s := range c.Sections {
		if slices.ContainsFunc(s.Checks, func(ch Check) bool { return ch.ID == id }) {
			return true
		}
	}
	return false
}

func goFiles(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	names := []string{}
	for _, f := range files {
		if name := filepath.Base(f); name != "solution.go" {
			names = append(names, name)
		}
	}
	return names
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
