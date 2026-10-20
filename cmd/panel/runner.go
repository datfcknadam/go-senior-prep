package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

type Runner struct {
	root string
	sem  chan struct{} // ограничение числа одновременных go test
}

func newRunner(root string, parallel int) *Runner {
	return &Runner{root: root, sem: make(chan struct{}, parallel)}
}

type TestResult struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"` // pass | fail | skip | notrun
	Elapsed float64 `json:"elapsed"`
	Output  string  `json:"output,omitempty"`
}

type RunResult struct {
	Task        string       `json:"task"`
	Solution    bool         `json:"solution"`
	Passed      bool         `json:"passed"`
	Ok          int          `json:"ok"`
	Total       int          `json:"total"`
	Tests       []TestResult `json:"tests"`
	BuildOutput string       `json:"buildOutput,omitempty"`
	Todo        bool         `json:"todo"` // упало на panic("TODO") в заготовке
	Duration    float64      `json:"duration"`
	Command     string       `json:"command"`
}

// testEvent — строка вывода `go test -json` (cmd/test2json).
type testEvent struct {
	Action  string
	Test    string
	Elapsed float64
	Output  string
}

const maxOutputLines = 60

func (r *Runner) Run(ctx context.Context, task Task, solution bool) (*RunResult, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	args := []string{"test", "-json", "-race", "-count=1", "-timeout=2m"}
	if solution {
		args = append(args, "-tags=solution")
	}
	args = append(args, "./"+task.Path)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = r.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	_ = runErr // ненулевой код выхода при упавших тестах — штатная ситуация, разбираем вывод

	res := parseTestJSON(&stdout, task)
	res.Solution = solution
	res.Duration = time.Since(start).Seconds()
	res.Command = "go " + strings.Join(args, " ")
	if s := strings.TrimSpace(stderr.String()); s != "" {
		res.BuildOutput = strings.TrimSpace(res.BuildOutput + "\n" + s)
	}
	return res, nil
}

func parseTestJSON(out *bytes.Buffer, task Task) *RunResult {
	res := &RunResult{Task: task.Path}
	status := map[string]string{}
	elapsed := map[string]float64{}
	output := map[string]*strings.Builder{}
	var order []string
	var build strings.Builder

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var ev testEvent
		if json.Unmarshal(line, &ev) != nil {
			build.Write(line)
			build.WriteByte('\n')
			continue
		}
		switch {
		case ev.Action == "build-output":
			build.WriteString(ev.Output)
		case ev.Test == "":
			// Вывод уровня пакета: паника вне теста, итоговые FAIL/ok.
			if ev.Action == "output" && !isNoise(ev.Output) {
				build.WriteString(ev.Output)
			}
		case ev.Action == "run":
			if _, seen := status[ev.Test]; !seen {
				order = append(order, ev.Test)
			}
			status[ev.Test] = "run"
		case ev.Action == "output":
			b := output[ev.Test]
			if b == nil {
				b = &strings.Builder{}
				output[ev.Test] = b
			}
			b.WriteString(ev.Output)
		case ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip":
			status[ev.Test] = ev.Action
			elapsed[ev.Test] = ev.Elapsed
		}
	}

	if strings.Contains(build.String(), "panic: TODO") {
		res.Todo = true
	}

	for _, name := range order {
		st := status[name]
		if st == "run" { // тест начался, но бинарь упал (паника) — считаем проваленным
			st = "fail"
		}
		tr := TestResult{Name: name, Status: st, Elapsed: elapsed[name]}
		if st == "fail" && output[name] != nil {
			tr.Output = trimOutput(output[name].String())
			if strings.Contains(tr.Output, "panic: TODO") {
				res.Todo = true
			}
		}
		res.Tests = append(res.Tests, tr)
	}
	// Тесты, до которых не дошло дело (обычно из-за паники в предыдущем).
	for _, name := range task.Tests {
		if _, ok := status[name]; !ok {
			res.Tests = append(res.Tests, TestResult{Name: name, Status: "notrun"})
		}
	}

	for _, t := range res.Tests {
		if strings.Contains(t.Name, "/") {
			continue // подтесты не входят в счёт
		}
		res.Total++
		if t.Status == "pass" {
			res.Ok++
		}
	}
	res.Passed = res.Total > 0 && res.Ok == res.Total
	res.BuildOutput = trimOutput(build.String())
	return res
}

func isNoise(s string) bool {
	t := strings.TrimSpace(s)
	return t == "PASS" || t == "FAIL" || strings.HasPrefix(t, "ok ") || strings.HasPrefix(t, "FAIL\t") ||
		strings.HasPrefix(t, "ok\t") || strings.HasPrefix(t, "=== ")
}

func trimOutput(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	kept := lines[:0]
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "=== PAUSE") || strings.HasPrefix(t, "=== CONT") {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) > maxOutputLines {
		kept = append(kept[:maxOutputLines], "… (вывод обрезан)")
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
