"use strict";

// Песочница: каждый ```go-блок в теории становится редактором с запуском.
// Блок, который начинается с `// exercise:`, — упражнение: вывод сверяет сервер,
// при совпадении упражнение отмечается в прогрессе.

const DIRECTIVE_RE = /^\/\/ (exercise|expect|hint|flags):?(.*)$/;
const HAS_TESTS_RE = /^func (Test|Benchmark|Fuzz)\w*\(/m;
const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
const runKey = isMac ? "⌘↵" : "Ctrl+↵";

function parseDirectives(text) {
  const lines = text.replace(/\n$/, "").split("\n");
  const meta = { title: "", expect: [], hints: [], race: false };
  let i = 0;
  for (; i < lines.length; i++) {
    const m = lines[i].match(DIRECTIVE_RE);
    if (!m) break;
    const val = m[2].replace(/^ /, "");
    if (m[1] === "exercise") meta.title = val.trim();
    else if (m[1] === "expect") meta.expect.push(val);
    else if (m[1] === "hint") meta.hints.push(val.trim());
    else if (m[1] === "flags") meta.race ||= val.includes("race");
  }
  if (!meta.title) return { meta: null, code: text.replace(/\n$/, "") };
  return { meta, code: lines.slice(i).join("\n") };
}

const slug = (s) => s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, "-").replace(/^-|-$/g, "");
const draftKey = (id) => `draft:${id}`;

function makeEditor(host, value, { onRun, lineNumbers = false, minLines = 0 }) {
  const runCmd = () => onRun("run");
  const cm = CodeMirror(host, {
    value,
    mode: "go",
    indentUnit: 4,
    tabSize: 4,
    indentWithTabs: true,
    lineNumbers,
    viewportMargin: Infinity,
    matchBrackets: true,
    autoCloseBrackets: true,
    extraKeys: {
      "Cmd-Enter": runCmd,
      "Ctrl-Enter": runCmd,
      "Shift-Cmd-Enter": () => onRun("race"),
      "Shift-Ctrl-Enter": () => onRun("race"),
      "Cmd-/": "toggleComment",
      "Ctrl-/": "toggleComment",
      Esc: (cm) => cm.getInputField().blur(),
    },
  });
  if (minLines) cm.getWrapperElement().style.minHeight = `${minLines * 1.55}em`;
  return cm;
}

// ---------- виджет ----------

function mountPlaygrounds(el, docPath) {
  const blocks = [...el.querySelectorAll("pre > code.language-go")];
  blocks.forEach((code, index) => {
    const { meta, code: src } = parseDirectives(code.textContent);
    const id = meta ? `${docPath}#ex:${meta.title}` : `${docPath}#${index}`;
    mountPlayground(code.parentElement, { id, src, meta });
  });
}

function mountPlayground(pre, { id, src, meta }) {
  const box = document.createElement("div");
  box.className = "play" + (meta ? " exercise" : "");
  if (meta) box.id = "ex-" + slug(meta.title);
  pre.replaceWith(box);

  const hasTests = () => HAS_TESTS_RE.test(cm.getValue());
  box.innerHTML = `
    ${meta ? `<div class="play-head">
        <span class="play-label">Упражнение</span>
        <span class="play-title">${esc(meta.title)}</span>
        <span class="pill ex-status"></span>
      </div>
      ${meta.expect.length ? `<div class="play-expect"><div class="side-title">Ожидаемый вывод</div><pre>${esc(meta.expect.join("\n"))}</pre></div>` : ""}` : ""}
    <div class="play-editor"></div>
    <div class="play-bar">
      <button class="btn small primary" data-mode="run" title="go run (${runKey})">▶ Запустить</button>
      <button class="btn small" data-mode="race" title="go run -race (Shift+${runKey})">-race</button>
      <button class="btn small" data-mode="escape" title="go build -gcflags=-m: что уходит в кучу">escape</button>
      <button class="btn small" data-mode="test" title="go test -v -bench . -benchmem" hidden>go test</button>
      <span class="play-spacer"></span>
      ${meta?.hints.length ? `<button class="btn small ghost" data-act="hint">Подсказка <span class="hint-n">0/${meta.hints.length}</span></button>` : ""}
      <button class="btn small ghost" data-act="reset" hidden title="Вернуть исходный код">↺ Сброс</button>
      <button class="btn small ghost" data-act="sandbox" title="Открыть в отдельной песочнице">⧉</button>
    </div>
    <ol class="play-hints"></ol>
    <div class="play-out" hidden></div>`;

  const saved = storageGet(draftKey(id));
  const cm = makeEditor(box.querySelector(".play-editor"), saved ?? src, {
    onRun: (mode) => run(mode),
    lineNumbers: Boolean(meta),
  });
  const resetBtn = box.querySelector('[data-act="reset"]');
  const testBtn = box.querySelector('[data-mode="test"]');
  const sync = () => {
    const dirty = cm.getValue() !== src;
    resetBtn.hidden = !dirty;
    box.classList.toggle("dirty", dirty);
    testBtn.hidden = !hasTests();
  };
  let saveTimer;
  cm.on("change", () => {
    sync();
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      if (cm.getValue() === src) storageDel(draftKey(id));
      else storageSet(draftKey(id), cm.getValue());
    }, 300);
  });
  sync();
  refreshExerciseStatus(box, id);

  let shownHints = 0;
  box.querySelector(".play-bar").addEventListener("click", (e) => {
    const btn = e.target.closest("button");
    if (!btn) return;
    if (btn.dataset.mode) return run(btn.dataset.mode);
    switch (btn.dataset.act) {
      case "reset":
        if (confirm("Вернуть исходный код? Твои правки пропадут.")) { cm.setValue(src); storageDel(draftKey(id)); }
        break;
      case "sandbox":
        openSandbox(cm.getValue());
        break;
      case "hint":
        if (shownHints < meta.hints.length) {
          box.querySelector(".play-hints").insertAdjacentHTML("beforeend", `<li>${esc(meta.hints[shownHints++])}</li>`);
          btn.querySelector(".hint-n").textContent = `${shownHints}/${meta.hints.length}`;
          if (shownHints === meta.hints.length) btn.disabled = true;
        }
        break;
    }
  });

  let busy = false;
  async function run(mode) {
    if (busy) return;
    busy = true;
    const out = box.querySelector(".play-out");
    box.querySelectorAll(".play-bar button").forEach((b) => (b.disabled = b.dataset.act !== "hint" ? true : b.disabled));
    const btn = box.querySelector(`[data-mode="${mode}"]`);
    const label = btn.innerHTML;
    btn.innerHTML = `<span class="spinner"></span>`;
    out.hidden = false;
    out.classList.add("stale");
    try {
      const res = await api("/api/play", { code: cm.getValue(), mode, exercise: meta ? id : "" });
      state.progress = res.progress;
      out.innerHTML = renderPlayResult(res.result);
      if (res.result.exercise?.passed) { renderChrome(); refreshExerciseStatus(box, id); }
    } catch (err) {
      out.innerHTML = `<div class="play-status fail">Ошибка: ${esc(err.message)}</div>`;
    } finally {
      out.classList.remove("stale");
      btn.innerHTML = label;
      box.querySelectorAll(".play-bar button").forEach((b) => { if (b.dataset.act !== "hint") b.disabled = false; });
      busy = false;
    }
  }
}

function refreshExerciseStatus(box, id) {
  const pill = box.querySelector(".ex-status");
  if (!pill) return;
  const done = isChecked(id);
  pill.className = `pill ex-status ${done ? "pass" : ""}`;
  pill.textContent = done ? "✓ решено" : "не решено";
  box.classList.toggle("solved", done);
}

// ---------- результат ----------

function renderPlayResult(r) {
  let status, cls;
  if (r.timeout) [status, cls] = [r.phase === "build" ? "Сборка не уложилась в таймаут" : "Таймаут: программа работала дольше лимита и была остановлена", "fail"];
  else if (r.phase === "build" && r.exitCode !== 0) [status, cls] = ["Не компилируется", "fail"];
  else if (r.mode === "escape") [status, cls] = ["Escape analysis (-gcflags=-m)", "info"];
  else if (r.exitCode === 0) [status, cls] = ["Выполнено", "pass"];
  else if (r.exitCode === 66 && /DATA RACE/.test(r.output)) [status, cls] = ["Race detector нашёл гонку", "fail"];
  else [status, cls] = [`Завершилось с кодом ${r.exitCode}`, "fail"];

  const ex = r.exercise;
  if (ex && cls === "pass") {
    [status, cls] = ex.passed ? ["Вывод совпал — упражнение засчитано", "pass"] : ["Вывод не совпадает с ожидаемым", "fail"];
  }
  let html = `<div class="play-status ${cls}">${cls === "pass" ? "✓" : cls === "fail" ? "✗" : "ℹ"} ${status}<span class="dur">${r.duration.toFixed(2)} с</span></div>`;
  if (ex && !ex.passed) html += renderDiff(ex.expect, ex.got);

  const output = r.output;
  if (output.trim()) html += `<pre class="play-pre">${highlightOutput(output, r.mode)}</pre>`;
  else if (!ex) html += `<div class="play-empty">Программа ничего не вывела.</div>`;
  if (r.truncated) html += `<div class="play-note">Вывод обрезан до 64 КБ.</div>`;

  if (r.source) html += `<details class="play-src"><summary>Код, который запускался (сниппет обёрнут в main, импорты дописаны)</summary><pre>${esc(r.source)}</pre></details>`;
  html += `<div class="play-cmd">$ ${esc(r.command)}</div>`;
  return html;
}

function renderDiff(expect, got) {
  const e = expect.split("\n"), g = got.split("\n");
  const n = Math.max(e.length, g.length);
  let rows = "";
  for (let i = 0; i < n; i++) {
    const bad = e[i] !== g[i];
    rows += `<tr class="${bad ? "bad" : ""}"><td class="ln">${i + 1}</td><td>${e[i] === undefined ? `<i>—</i>` : esc(e[i]) || "&nbsp;"}</td><td>${g[i] === undefined ? `<i>—</i>` : esc(g[i]) || "&nbsp;"}</td></tr>`;
  }
  return `<table class="play-diff"><thead><tr><th></th><th>ожидалось</th><th>получено (stdout)</th></tr></thead><tbody>${rows}</tbody></table>`;
}

function highlightOutput(out, mode) {
  return esc(out).split("\n").map((l) => {
    if (/escapes to heap|moved to heap/.test(l)) return `<span class="o-heap">${l}</span>`;
    if (/does not escape|can inline|inlining call/.test(l)) return `<span class="o-dim">${l}</span>`;
    if (/^(WARNING: DATA RACE|panic:|fatal error:|--- FAIL)/.test(l) || /\.go:\d+:\d+: /.test(l) && mode !== "escape") return `<span class="o-err">${l}</span>`;
    if (/^(--- PASS|ok |PASS$)/.test(l)) return `<span class="o-ok">${l}</span>`;
    if (/^Benchmark/.test(l)) return `<span class="o-bench">${l}</span>`;
    return l;
  }).join("\n");
}

// ---------- страница песочницы ----------

const SANDBOX_TEMPLATES = {
  "Пустая программа": `package main

import "fmt"

func main() {
	fmt.Println("Hello, Gopher!")
}
`,
  "Гонка данных (запусти с -race)": `package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	counter := 0
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter++ // гонка: почини через sync.Mutex или atomic.Int64
		}()
	}
	wg.Wait()
	fmt.Println(counter)
}
`,
  "Escape analysis": `package main

type point struct{ x, y int }

func onStack() int {
	p := point{1, 2} // не убегает
	return p.x + p.y
}

func onHeap() *point {
	return &point{1, 2} // убегает: указатель переживает функцию
}

func viaInterface(v any) {} // интерфейс часто заставляет значение уйти в кучу

func main() {
	_ = onStack()
	_ = onHeap()
	viaInterface(42)
}
`,
  "Бенчмарк (go test)": `package main

import (
	"strings"
	"testing"
)

func concat(n int) string {
	s := ""
	for range n {
		s += "x"
	}
	return s
}

func builder(n int) string {
	var b strings.Builder
	b.Grow(n)
	for range n {
		b.WriteByte('x')
	}
	return b.String()
}

func TestSame(t *testing.T) {
	if concat(100) != builder(100) {
		t.Fatal("разный результат")
	}
}

func BenchmarkConcat(b *testing.B) {
	for b.Loop() {
		concat(1000)
	}
}

func BenchmarkBuilder(b *testing.B) {
	for b.Loop() {
		builder(1000)
	}
}
`,
  "Утечка горутины": `package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

func producer(ctx context.Context) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 0; ; i++ {
			ch <- i // без select на ctx.Done() горутина зависнет навсегда
		}
	}()
	return ch
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	for v := range producer(ctx) {
		if v == 3 {
			break
		}
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	fmt.Println("горутин:", runtime.NumGoroutine()) // должно быть 1
}
`,
};

const SANDBOX_KEY = "sandbox:code";

function openSandbox(code) {
  storageSet(SANDBOX_KEY, code);
  location.hash = "#/play";
}

function pagePlay() {
  const code = storageGet(SANDBOX_KEY) ?? SANDBOX_TEMPLATES["Пустая программа"];
  main().innerHTML = `
    <div class="page-head">
      <div class="crumbs"><a href="#/">Обзор</a> / песочница</div>
      <h1>Песочница</h1>
      <div class="sub">Локальный <code>go run</code> на твоей машине: можно <code>-race</code>, escape analysis и <code>go test</code> с бенчмарками —
      то, чего нет в go.dev/play. Код сохраняется в браузере. Запуск — <kbd>${runKey}</kbd>, с race-детектором — <kbd>Shift+${runKey}</kbd>.</div>
    </div>
    <div class="sandbox-tools">
      <label>Шаблон
        <select id="tpl"><option value="">— выбрать —</option>${Object.keys(SANDBOX_TEMPLATES).map((k) => `<option>${esc(k)}</option>`).join("")}</select>
      </label>
    </div>
    <div id="sandbox"></div>`;

  const host = $("#sandbox");
  host.innerHTML = `<pre><code class="language-go"></code></pre>`;
  host.querySelector("code").textContent = code;
  mountPlayground(host.querySelector("pre"), { id: "sandbox", src: code, meta: null });
  const box = host.querySelector(".play");
  box.classList.add("big");
  const cm = box.querySelector(".CodeMirror").CodeMirror;
  cm.setOption("lineNumbers", true);
  box.querySelector('[data-act="sandbox"]').remove();
  box.querySelector('[data-act="reset"]')?.remove();
  storageDel(draftKey("sandbox"));
  cm.on("change", () => storageSet(SANDBOX_KEY, cm.getValue()));
  cm.focus();
  cm.setCursor(cm.lineCount(), 0);

  $("#tpl").addEventListener("change", (e) => {
    const t = SANDBOX_TEMPLATES[e.target.value];
    if (!t) return;
    if (cm.getValue().trim() && !confirm("Заменить текущий код шаблоном?")) { e.target.value = ""; return; }
    cm.setValue(t);
    e.target.value = "";
  });
}
