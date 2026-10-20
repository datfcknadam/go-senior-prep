"use strict";

// ---------- состояние ----------

const state = {
  course: null,
  progress: { tasks: {}, checks: {} },
  results: {},   // task path → последний RunResult в этой сессии
  running: {},   // task path → "task" | "solution"
};

const $ = (sel, root = document) => root.querySelector(sel);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

async function api(path, body) {
  const opts = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
  const res = await fetch(path, opts);
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch { /* не JSON */ }
    throw new Error(msg);
  }
  return res.headers.get("Content-Type")?.includes("json") ? res.json() : res.text();
}

// ---------- тема ----------

function storageGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function storageSet(k, v) { try { localStorage.setItem(k, v); } catch { /* приватный режим */ } }
function storageDel(k) { try { localStorage.removeItem(k); } catch { /* приватный режим */ } }

function applyTheme(theme) {
  if (theme) document.documentElement.dataset.theme = theme;
  else delete document.documentElement.dataset.theme;
  const dark = theme ? theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  $("#hl-light").disabled = dark;
  $("#hl-dark").disabled = !dark;
}

$("#theme-btn").addEventListener("click", () => {
  const cur = document.documentElement.dataset.theme
    || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const next = cur === "dark" ? "light" : "dark";
  storageSet("theme", next);
  applyTheme(next);
});
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => applyTheme(storageGet("theme")));
applyTheme(storageGet("theme"));

$("#menu-btn").addEventListener("click", () => document.body.classList.toggle("nav-open"));

// ---------- прогресс ----------

const allTasks = () => state.course.sections.flatMap((s) => s.tasks);
const allChecks = () => state.course.sections.flatMap((s) => s.checks);
const taskStatus = (path) => {
  const p = state.progress.tasks[path];
  if (!p) return "idle";
  if (p.passed) return "pass";
  return p.passedOnce ? "once" : "fail";
};
const isChecked = (id) => Boolean(state.progress.checks[id]);
const allExercises = () => allChecks().filter((c) => c.kind === "exercise");

function sectionStats(s) {
  const tasksDone = s.tasks.filter((t) => taskStatus(t.path) === "pass").length;
  const checksDone = s.checks.filter((c) => isChecked(c.id)).length;
  const total = s.tasks.length + s.checks.length;
  return { tasksDone, checksDone, done: tasksDone + checksDone, total };
}

function totalStats() {
  const tasks = allTasks(), checks = allChecks();
  const tasksDone = tasks.filter((t) => taskStatus(t.path) === "pass").length;
  const checksDone = checks.filter((c) => isChecked(c.id)).length;
  const total = tasks.length + checks.length;
  const ex = allExercises();
  return { exercises: ex.length, exercisesDone: ex.filter((c) => isChecked(c.id)).length,
    tasks: tasks.length, tasksDone, checks: checks.length, checksDone, pct: total ? Math.round(((tasksDone + checksDone) / total) * 100) : 0 };
}

const bar = (pct, ok) => `<div class="bar${ok ? " ok" : ""}"><span style="width:${pct}%"></span></div>`;
const pct = (a, b) => (b ? Math.round((a / b) * 100) : 0);
const sectionNum = (dir) => dir.slice(0, 2);

function timeAgo(iso) {
  if (!iso) return "";
  const sec = (Date.now() - new Date(iso).getTime()) / 1000;
  if (sec < 60) return "только что";
  if (sec < 3600) return `${Math.floor(sec / 60)} мин назад`;
  if (sec < 86400) return `${Math.floor(sec / 3600)} ч назад`;
  return new Date(iso).toLocaleDateString("ru-RU", { day: "numeric", month: "short" });
}

// ---------- шапка и сайдбар ----------

function renderChrome() {
  const t = totalStats();
  $("#top-progress").innerHTML = `<span>${t.pct}% курса</span>${bar(t.pct)}`;

  const route = decodeURIComponent(location.hash.slice(1)).split("#")[0] || "/";
  const active = (href) => (route === href.slice(1) ? " active" : "");

  let html = `<a class="nav-home${active("#/")}" href="#/">◎ Обзор</a>
    <a class="nav-home${active("#/play")}" href="#/play">▷ Песочница</a>`;
  for (const s of state.course.sections) {
    const st = sectionStats(s);
    const inSection = route.includes(`/${s.dir}`);
    html += `<div class="nav-section">
      <a class="${active(`#/s/${s.dir}`).trim()}" href="#/s/${s.dir}">
        <span class="nav-num">${sectionNum(s.dir)}</span>
        <span class="nav-title">${esc(s.title)}</span>
        ${st.total ? `<span class="nav-count">${st.done}/${st.total}</span>` : ""}
      </a>`;
    const children = [
      ...s.tasks.map((t) => `<a class="nav-item${active(`#/t/${t.path}`)}" href="#/t/${t.path}"><span class="dot ${taskStatus(t.path)}"></span><span class="nav-title">${esc(t.title)}</span></a>`),
      ...s.docs.map((d) => `<a class="nav-item${active(`#/d/${d.path}`)}" href="#/d/${d.path}"><span class="doc-icon">≡</span><span class="nav-title">${esc(d.title)}</span></a>`),
    ];
    if (children.length && (inSection || s.tasks.length)) html += `<div class="nav-children">${children.join("")}</div>`;
    html += `</div>`;
  }
  $("#sidebar").innerHTML = html;
}

// ---------- markdown ----------

const mdCache = {};

async function loadFile(path, reveal) {
  const key = path + (reveal ? "?r" : "");
  if (!(key in mdCache)) mdCache[key] = await api(`/api/file?path=${encodeURIComponent(path)}${reveal ? "&reveal=1" : ""}`);
  return mdCache[key];
}

function resolvePath(base, href) {
  const parts = base ? base.split("/") : [];
  for (const p of href.split("/")) {
    if (p === "" || p === ".") continue;
    if (p === "..") parts.pop();
    else parts.push(p);
  }
  return parts.join("/");
}

function routeForPath(p) {
  p = p.replace(/\/$/, "");
  if (allTasks().some((t) => t.path === p)) return `#/t/${p}`;
  if (state.course.sections.some((s) => s.dir === p)) return `#/s/${p}`;
  if (p.endsWith("/README.md")) return routeForPath(p.slice(0, -"/README.md".length)) || `#/d/${p}`;
  if (p.endsWith(".md")) return `#/d/${p}`;
  if (/\.(go|sql)$/.test(p)) return `#/f/${p}`;
  return null;
}

function renderMarkdown(el, text, docPath, { dropTitle = false } = {}) {
  el.innerHTML = marked.parse(text);
  if (dropTitle) el.querySelector("h1")?.remove();
  const base = docPath.split("/").slice(0, -1).join("/");

  for (const a of el.querySelectorAll("a[href]")) {
    const href = a.getAttribute("href");
    if (/^[a-z]+:/i.test(href)) { a.target = "_blank"; a.rel = "noopener"; continue; }
    if (href.startsWith("#")) continue;
    const route = routeForPath(resolvePath(base, href.split("#")[0]));
    if (route) a.setAttribute("href", route);
  }
  for (const code of el.querySelectorAll("pre code:not(.language-go)")) hljs.highlightElement(code);
  mountPlaygrounds(el, docPath);

  // Чек-боксы у заголовков ###, если они — пункты чек-листа.
  const checks = new Set(allChecks().filter((c) => c.doc === docPath).map((c) => c.id));
  if (!checks.size) return;
  for (const h of el.querySelectorAll("h3")) {
    const id = `${docPath}#${h.textContent.trim()}`;
    if (!checks.has(id)) continue;
    const text = h.innerHTML;
    h.innerHTML = `<input type="checkbox" class="h-check" data-check="${esc(id)}"${isChecked(id) ? " checked" : ""}><span>${text}</span>`;
    h.classList.toggle("checked", isChecked(id));
  }
}

// ---------- действия ----------

document.addEventListener("change", async (e) => {
  const id = e.target.dataset?.check;
  if (!id) return;
  e.target.disabled = true;
  try {
    const res = await api("/api/check", { id, done: e.target.checked });
    state.progress = res.progress;
    e.target.closest("label")?.classList.toggle("done", e.target.checked);
    e.target.closest("h3")?.classList.toggle("checked", e.target.checked);
    renderChrome();
  } catch (err) {
    e.target.checked = !e.target.checked;
    alert(`Не удалось сохранить: ${err.message}`);
  } finally {
    e.target.disabled = false;
  }
});

async function runTests(path, solution) {
  if (state.running[path]) return;
  state.running[path] = solution ? "solution" : "task";
  refreshRunPanel(path);
  try {
    const res = await api("/api/run", { task: path, solution });
    state.results[path] = res.result;
    state.progress = res.progress;
  } catch (err) {
    state.results[path] = { error: err.message, solution };
  } finally {
    delete state.running[path];
    renderChrome();
    refreshRunPanel(path);
  }
}

// ---------- страницы ----------

const main = () => $("#main");

function pageDashboard() {
  const t = totalStats();
  const r = 52, c = 2 * Math.PI * r;
  const next = allTasks().find((x) => taskStatus(x.path) !== "pass");
  const nextSection = next && state.course.sections.find((s) => s.tasks.includes(next));

  const recent = Object.entries(state.progress.tasks)
    .filter(([, p]) => p.lastRun)
    .sort((a, b) => new Date(b[1].lastRun) - new Date(a[1].lastRun))
    .slice(0, 6);
  const taskTitle = (p) => allTasks().find((x) => x.path === p)?.title || p;

  main().innerHTML = `
    <section class="hero">
      <svg class="ring" viewBox="0 0 120 120" role="img" aria-label="Пройдено ${t.pct}%">
        <circle class="track" cx="60" cy="60" r="${r}"></circle>
        <circle class="value" cx="60" cy="60" r="${r}" stroke-dasharray="${c}" stroke-dashoffset="${c * (1 - t.pct / 100)}" transform="rotate(-90 60 60)"></circle>
        <text x="60" y="68" text-anchor="middle">${t.pct}%</text>
      </svg>
      <div>
        <h1>Подготовка к собеседованию на Go Senior</h1>
        <div class="sub" style="color:var(--muted)">Теория, задачи с тестами, чек-листы и mock-интервью.</div>
        <div class="hero-stats">
          <div class="stat"><b>${t.tasksDone}/${t.tasks}</b><span>задач решено</span></div>
          <div class="stat"><b>${t.exercisesDone}/${t.exercises}</b><span>упражнений в песочнице</span></div>
          <div class="stat"><b>${t.checksDone - t.exercisesDone}/${t.checks - t.exercises}</b><span>пунктов чек-листа</span></div>
          <div class="stat"><b>${state.course.sections.length}</b><span>разделов</span></div>
        </div>
      </div>
    </section>

    ${next ? `
    <div class="card next-card">
      <div>
        <div class="label">Следующая задача</div>
        <div class="title">${esc(next.title)}</div>
        <div style="color:var(--muted);font-size:13px">${esc(nextSection.title)} · <code style="font-family:var(--mono)">${esc(next.path)}/task.go</code></div>
      </div>
      <a class="btn primary" href="#/t/${next.path}">Открыть →</a>
    </div>` : `
    <div class="card next-card"><div><div class="label">Все задачи решены</div><div class="title">Время для mock-интервью и финального проекта</div></div>
      <a class="btn primary" href="#/s/08-interview">К интервью →</a></div>`}

    <h2 class="block-title">Разделы</h2>
    <div class="grid">
      ${state.course.sections.map((s) => {
        const st = sectionStats(s);
        const parts = [];
        if (s.tasks.length) parts.push(`задачи ${st.tasksDone}/${s.tasks.length}`);
        if (s.checks.length) parts.push(`чек-лист ${st.checksDone}/${s.checks.length}`);
        return `<a class="card section-card" href="#/s/${s.dir}">
          <div class="num">${sectionNum(s.dir)}</div>
          <div class="name">${esc(s.title)}</div>
          ${st.total ? bar(pct(st.done, st.total), st.done === st.total) : `<div class="bar"></div>`}
          <div class="meta"><span>${parts.join(" · ") || "теория"}</span><span>${st.total ? pct(st.done, st.total) + "%" : ""}</span></div>
        </a>`;
      }).join("")}
    </div>

    ${recent.length ? `
    <h2 class="block-title">Последние запуски</h2>
    <div class="card" style="padding:4px 20px">
      <ul class="activity">
        ${recent.map(([p, pr]) => `<li><span class="dot ${taskStatus(p)}"></span><a href="#/t/${p}">${esc(taskTitle(p))}</a>
          <span class="pill ${pr.passed ? "pass" : "fail"}">${pr.ok}/${pr.total}</span><span class="when">${timeAgo(pr.lastRun)}</span></li>`).join("")}
      </ul>
    </div>` : ""}
  `;
}

async function pageSection(dir) {
  const s = state.course.sections.find((x) => x.dir === dir);
  if (!s) return pageNotFound();
  const st = sectionStats(s);

  main().innerHTML = `
    <div class="page-head">
      <div class="crumbs"><a href="#/">Обзор</a> / раздел ${sectionNum(dir)}</div>
      <h1>${esc(s.title)}</h1>
      ${st.total ? `<div style="max-width:360px;margin-top:8px">${bar(pct(st.done, st.total), st.done === st.total)}</div>
      <div class="sub">${st.done} из ${st.total} выполнено</div>` : ""}
    </div>
    ${s.tasks.length ? `<h2 class="block-title">Задачи</h2>
      <div class="task-list">${s.tasks.map((t) => {
        const p = state.progress.tasks[t.path];
        return `<a class="card task-row" href="#/t/${t.path}">
          <span class="dot ${taskStatus(t.path)}"></span>
          <span class="t-title">${esc(t.title)}<div class="t-path">${esc(t.path)}</div></span>
          ${p ? `<span class="pill ${p.passed ? "pass" : "fail"}">${p.ok}/${p.total}</span>` : `<span class="pill">${t.tests.length} тестов</span>`}
        </a>`;
      }).join("")}</div>` : ""}
    ${s.docs.length ? `<h2 class="block-title">Материалы</h2>
      <div class="task-list">${s.docs.map((d) => `<a class="card task-row" href="#/d/${d.path}"><span class="doc-icon">≡</span><span class="t-title">${esc(d.title)}<div class="t-path">${esc(d.path)}</div></span></a>`).join("")}</div>` : ""}
    ${exercisesBlock(s.checks.filter((c) => c.kind === "exercise"))}
    ${s.checks.some((c) => c.kind !== "exercise") ? `<h2 class="block-title">Чек-лист</h2>
      <div class="card" style="padding:6px 16px"><ul class="checklist">${s.checks.filter((c) => c.kind !== "exercise").map((c) => `
        <li><label class="${isChecked(c.id) ? "done" : ""}">
          <input type="checkbox" data-check="${esc(c.id)}"${isChecked(c.id) ? " checked" : ""}>
          <span>${esc(c.title)}</span>
          ${c.doc && c.doc !== s.readme ? `<a class="goto" href="#/d/${c.doc}">открыть →</a>` : ""}
        </label></li>`).join("")}</ul></div>` : ""}
    <h2 class="block-title">Теория</h2>
    <article class="md card" id="doc"><div class="loading">Загрузка…</div></article>
  `;
  renderMarkdown($("#doc"), await loadFile(s.readme), s.readme, { dropTitle: true });
}

// Упражнения раздела, сгруппированные по главам.
function exercisesBlock(list) {
  if (!list.length) return "";
  const byDoc = Map.groupBy(list, (c) => c.doc);
  const docTitle = (p) => state.course.sections.flatMap((s) => s.docs).find((d) => d.path === p)?.title || p;
  const done = list.filter((c) => isChecked(c.id)).length;
  return `<h2 class="block-title">Упражнения в песочнице · ${done}/${list.length}</h2>
    <div class="card" style="padding:6px 16px"><ul class="checklist ex-list">${[...byDoc].map(([doc, items]) => `
      <li class="ex-group"><a href="#/d/${doc}">${esc(docTitle(doc))}</a>
        <span class="ex-chips">${items.map((c) => `<a class="ex-chip${isChecked(c.id) ? " done" : ""}" href="#/d/${doc}#ex-${slug(c.title)}" title="${esc(c.title)}">${isChecked(c.id) ? "✓ " : ""}${esc(c.title)}</a>`).join("")}</span>
      </li>`).join("")}</ul></div>`;
}

async function pageTask(path) {
  const task = allTasks().find((t) => t.path === path);
  if (!task) return pageNotFound();
  const section = state.course.sections.find((s) => s.tasks.includes(task));

  main().innerHTML = `
    <div class="page-head">
      <div class="crumbs"><a href="#/">Обзор</a> / <a href="#/s/${section.dir}">${esc(section.title)}</a></div>
    </div>
    <div class="task-layout">
      <article class="md" id="doc"><div class="loading">Загрузка…</div></article>
      <aside class="task-side">
        <div class="card" id="run-panel"></div>
        <div class="card">
          <div class="side-title">Файлы</div>
          <div class="files">
            ${task.files.map((f) => `<a href="#/f/${path}/${f}">${esc(f)}</a>`).join("")}
            <a class="spoiler" href="#/f/${path}/solution.go" data-reveal>solution.go</a>
          </div>
          <div class="run-meta">Решение пиши в <code>${esc(path)}/task.go</code> в своей IDE и запускай тесты отсюда.</div>
        </div>
      </aside>
    </div>
  `;
  refreshRunPanel(path);
  renderMarkdown($("#doc"), await loadFile(`${path}/README.md`), `${path}/README.md`);
}

function refreshRunPanel(path) {
  const el = $("#run-panel");
  if (!el || location.hash !== `#/t/${path}`) return;
  const task = allTasks().find((t) => t.path === path);
  const running = state.running[path];
  const res = state.results[path];
  const saved = state.progress.tasks[path];

  let html = `<div class="side-title">Тесты</div>
    <div class="run-actions">
      <button class="btn primary" data-run="task"${running ? " disabled" : ""}>
        ${running === "task" ? `<span class="spinner"></span> Выполняется…` : "▶ Запустить тесты"}</button>
      <button class="btn" data-run="solution"${running ? " disabled" : ""} title="Прогнать эталонное решение (код не показывается)">
        ${running === "solution" ? `<span class="spinner"></span> Эталон…` : "Эталон"}</button>
    </div>`;

  if (running) {
    html += `<div class="run-meta">go test -race компилирует пакет с инструментированием — первый запуск может занять 10–30 секунд.</div>`;
  } else if (!res && saved) {
    html += `<div class="run-meta">Последний запуск ${timeAgo(saved.lastRun)}: <span class="pill ${saved.passed ? "pass" : "fail"}">${saved.ok}/${saved.total}</span></div>`;
  } else if (!res) {
    html += `<div class="run-meta">${task.tests.length} тестов: ${task.tests.map(esc).join(", ")}</div>`;
  }

  if (res && !running) html += renderResult(res);
  el.innerHTML = html;
}

function renderResult(res) {
  if (res.error) return `<div class="banner" style="background:var(--fail-soft);color:var(--fail)">Ошибка запуска: ${esc(res.error)}</div>`;
  const label = res.solution ? "Эталон: " : "";
  let html = `<div class="result-head ${res.passed ? "pass" : "fail"}" style="margin-top:16px">
      ${res.passed ? "✓" : "✗"} ${label}${res.ok}/${res.total} тестов
      <span class="dur">${res.duration.toFixed(1)} с</span></div>`;

  if (res.todo) html += `<div class="banner">Заготовка ещё не реализована: в <code>task.go</code> стоит <code>panic("TODO")</code>. После первой паники остальные тесты не запускаются.</div>`;
  else if (res.passed && !res.solution) html += `<div class="banner" style="background:var(--ok-soft);color:var(--ok)">Задача решена. Теперь сравни с эталоном и ответь на вопросы из условия.</div>`;

  if (res.buildOutput && !res.todo) html += `<div style="margin-top:12px"><div class="side-title">Вывод</div><pre class="out">${esc(res.buildOutput)}</pre></div>`;

  const icon = { pass: "✓", fail: "✗", skip: "–", notrun: "○" };
  html += `<ul class="tests">${res.tests.map((t) => {
    const sub = t.name.includes("/");
    const line = `<div class="test-line"><span class="ico ${t.status}">${icon[t.status] || "?"}</span>
      <span class="name${sub ? " sub" : ""}">${esc(sub ? t.name.split("/").slice(1).join("/") : t.name)}</span>
      ${t.status === "notrun" ? `<span class="el">не запускался</span>` : t.elapsed ? `<span class="el">${t.elapsed.toFixed(2)}s</span>` : ""}</div>`;
    return t.output
      ? `<li><details${res.tests.filter((x) => x.output).length === 1 ? " open" : ""}><summary>${line}</summary><pre class="out">${esc(t.output)}</pre></details></li>`
      : `<li>${line}</li>`;
  }).join("")}</ul>`;

  html += `<div class="cmd"><code title="${esc(res.command)}">$ ${esc(res.command)}</code>
    <button class="btn small" data-copy="${esc(res.command)}">Копировать</button></div>`;
  return html;
}

document.addEventListener("click", (e) => {
  const run = e.target.closest("[data-run]");
  if (run) {
    const path = location.hash.slice("#/t/".length);
    runTests(path, run.dataset.run === "solution");
    return;
  }
  const copy = e.target.closest("[data-copy]");
  if (copy) {
    navigator.clipboard?.writeText(copy.dataset.copy).then(() => {
      copy.textContent = "Скопировано";
      setTimeout(() => (copy.textContent = "Копировать"), 1200);
    });
    return;
  }
  const reveal = e.target.closest("[data-reveal]");
  if (reveal && !confirm("Открыть эталонное решение?\n\nЛучше сначала добиться зелёных тестов самостоятельно.")) {
    e.preventDefault();
    return;
  }
  if (e.target.closest(".sidebar a")) document.body.classList.remove("nav-open");
});

async function pageDoc(path, frag) {
  const section = state.course.sections.find((s) => path.startsWith(s.dir + "/"));
  const ex = allExercises().filter((c) => c.doc === path);
  const exDone = ex.filter((c) => isChecked(c.id)).length;
  main().innerHTML = `
    <div class="page-head"><div class="crumbs"><a href="#/">Обзор</a>${section ? ` / <a href="#/s/${section.dir}">${esc(section.title)}</a>` : ""}
      ${ex.length ? `<a class="ex-jump" href="#/d/${path}#ex-${slug(ex.find((c) => !isChecked(c.id))?.title ?? ex[0].title)}">▷ Упражнения ${exDone}/${ex.length}</a>` : ""}</div></div>
    <article class="md" id="doc"><div class="loading">Загрузка…</div></article>`;
  renderMarkdown($("#doc"), await loadFile(path), path);
  if (frag) document.getElementById(frag)?.scrollIntoView({ block: "start" });
}

async function pageFile(path) {
  const isSolution = path.endsWith("/solution.go");
  const parent = path.split("/").slice(0, -1).join("/");
  const back = routeForPath(parent) || "#/";
  main().innerHTML = `
    <div class="page-head">
      <div class="crumbs"><a href="#/">Обзор</a> / <a href="${back}">${esc(parent)}</a></div>
      <h1 style="font-family:var(--mono);font-size:22px">${esc(path.split("/").pop())}</h1>
      ${isSolution ? `<div class="sub">Эталонное решение. Сравни подходы: где отличается обработка отмены, ошибок и граничных случаев?</div>` : ""}
    </div>
    <div class="md code-view"><pre><code id="code" class="language-${path.endsWith(".sql") ? "sql" : "go"}">Загрузка…</code></pre></div>`;
  const code = $("#code");
  code.textContent = await loadFile(path, isSolution);
  hljs.highlightElement(code);
}

function pageNotFound() {
  main().innerHTML = `<div class="empty">Страница не найдена. <a href="#/">На главную</a></div>`;
}

// ---------- роутер ----------

async function route() {
  const [hash, frag] = (decodeURIComponent(location.hash.slice(1)) || "/").split("#");
  renderChrome();
  window.scrollTo(0, 0);
  try {
    if (hash === "/") pageDashboard();
    else if (hash === "/play") pagePlay();
    else if (hash.startsWith("/s/")) await pageSection(hash.slice(3));
    else if (hash.startsWith("/t/")) await pageTask(hash.slice(3));
    else if (hash.startsWith("/d/")) await pageDoc(hash.slice(3), frag);
    else if (hash.startsWith("/f/")) await pageFile(hash.slice(3));
    else pageNotFound();
  } catch (err) {
    main().innerHTML = `<div class="empty">Ошибка: ${esc(err.message)}</div>`;
  }
}

window.addEventListener("hashchange", route);

(async function init() {
  try {
    const data = await api("/api/state");
    state.course = data.course;
    state.progress = data.progress;
    route();
  } catch (err) {
    main().innerHTML = `<div class="empty">Не удалось загрузить курс: ${esc(err.message)}. Сервер панели запущен?</div>`;
  }
})();
