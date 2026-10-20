package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestWrapSnippet(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "full program untouched",
			in:   "package main\n\nfunc main() {}\n",
			want: "package main\n\nfunc main() {}\n",
		},
		{
			name: "statements go to main",
			in:   "x := 1\nfmt.Println(x)",
			want: "package main\n\nfunc main() {\n\tx := 1\n\tfmt.Println(x)\n}\n",
		},
		{
			name: "declarations stay top-level",
			in:   "type T struct{ a int }\nfunc (T) M() {\n\tif true {\n\t}\n}\n\nT{}.M()",
			want: "package main\n\ntype T struct{ a int }\n\nfunc (T) M() {\n\tif true {\n\t}\n}\n\nfunc main() {\n\n\tT{}.M()\n}\n",
		},
		{
			name: "braces inside strings and comments ignored",
			in:   "func f() string {\n\treturn \"}\" // }\n}\nfmt.Println(f())",
			want: "package main\n\nfunc f() string {\n\treturn \"}\" // }\n}\n\nfunc main() {\n\tfmt.Println(f())\n}\n",
		},
		{
			name: "only declarations without main",
			in:   "func add(a, b int) int { return a + b }",
			want: "package main\n\nfunc add(a, b int) int { return a + b }\n\nfunc main() {}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapSnippet(tt.in); got != tt.want {
				t.Errorf("wrapSnippet:\n--- got\n%s\n--- want\n%s", got, tt.want)
			}
		})
	}
}

func TestParseExercise(t *testing.T) {
	lines := []string{
		"// exercise: Разворот",
		"// expect: 3 2 1",
		"// expect:",
		"// expect: done",
		"// hint: подсказка",
		"// flags: race",
		"package main",
	}
	ex, ok := parseExercise(lines, "00-book/x.md")
	if !ok {
		t.Fatal("не распознано")
	}
	if ex.ID != "00-book/x.md#ex:Разворот" || ex.Expect != "3 2 1\n\ndone" || !ex.Race {
		t.Errorf("got %+v", ex)
	}
	if _, ok := parseExercise([]string{"package main"}, "x.md"); ok {
		t.Error("обычный блок принят за упражнение")
	}
}

func TestPlaygroundRun(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go не найден")
	}
	p := newPlayground("..", 1)
	ctx := context.Background()

	res, err := p.Run(ctx, PlayRequest{Mode: "run", Code: "fmt.Println(\"hi\")\nfmt.Fprintln(os.Stderr, \"err\")"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "hi\n" || !strings.Contains(res.Output, "err") {
		t.Errorf("run: %+v", res)
	}

	res, err = p.Run(ctx, PlayRequest{Mode: "run", Code: "x := 1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Phase != "build" || res.ExitCode == 0 || !strings.Contains(res.Output, "declared and not used") {
		t.Errorf("build error: %+v", res)
	}

	if testing.Short() {
		return
	}
	res, err = p.Run(ctx, PlayRequest{Mode: "run", Code: "for {}"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Timeout {
		t.Errorf("ожидали таймаут: %+v", res)
	}
}
