package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type API struct {
	root   string
	course *Course
	store  *Store
	runner *Runner
	play   *Playground
}

func (a *API) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", a.state)
	mux.HandleFunc("GET /api/file", a.file)
	mux.HandleFunc("POST /api/run", a.run)
	mux.HandleFunc("POST /api/check", a.check)
	mux.HandleFunc("POST /api/play", a.playground)
}

// localOnly защищает API от чужих страниц в браузере. Панель запускает произвольный код,
// поэтому без проверки любой сайт мог бы отправить «простой» POST с text/plain (CORS его
// не блокирует) или подменить DNS на 127.0.0.1 (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !isLoopbackHost(r.Host) {
			writeError(w, http.StatusForbidden, "доступ только через localhost")
			return
		}
		if r.Method != http.MethodGet {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				writeError(w, http.StatusForbidden, "чужой Origin")
				return
			}
			if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "нужен application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (a *API) playground(w http.ResponseWriter, r *http.Request) {
	var req PlayRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPlayCode+1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	var ex Exercise
	if req.Exercise != "" {
		var ok bool
		if ex, ok = a.course.exercise(req.Exercise); !ok {
			writeError(w, http.StatusNotFound, "упражнение не найдено")
			return
		}
		if ex.Race && req.Mode == "run" {
			req.Mode = "race"
		}
	}
	res, err := a.play.Run(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Exercise != "" && res.Phase == "run" {
		got := normalizeOutput(res.Stdout)
		res.Exercise = &ExerciseResult{Expect: ex.Expect, Got: got, Passed: got == ex.Expect && res.ExitCode == 0 && !res.Timeout}
		if res.Exercise.Passed {
			if err := a.store.SetCheck(ex.ID, true); err != nil {
				slog.Error("save progress", "err", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "progress": a.store.Snapshot()})
}

func (a *API) state(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"course":   a.course,
		"progress": a.store.Snapshot(),
	})
}

var allowedExt = map[string]bool{".md": true, ".go": true, ".sql": true}

// file отдаёт файлы курса. Эталонные решения — только с явным reveal=1.
func (a *API) file(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean("/" + r.URL.Query().Get("path"))[1:]
	if rel == "" || !allowedExt[path.Ext(rel)] || strings.Contains(rel, "/.") || strings.HasPrefix(rel, ".") {
		writeError(w, http.StatusBadRequest, "недопустимый путь")
		return
	}
	if path.Base(rel) == "solution.go" && r.URL.Query().Get("reveal") != "1" {
		writeError(w, http.StatusForbidden, "эталон скрыт")
		return
	}
	b, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "файл не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(b)
}

func (a *API) run(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Task     string `json:"task"`
		Solution bool   `json:"solution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	// Запускаем только задачи из курса — произвольный путь в go test не попадёт.
	task, ok := a.course.task(req.Task)
	if !ok {
		writeError(w, http.StatusNotFound, "задача не найдена")
		return
	}
	res, err := a.runner.Run(r.Context(), task, req.Solution)
	if err != nil {
		writeError(w, http.StatusGatewayTimeout, err.Error())
		return
	}
	if !req.Solution {
		tp := TaskProgress{Passed: res.Passed, Ok: res.Ok, Total: res.Total, LastRun: time.Now()}
		if err := a.store.SetTask(task.Path, tp); err != nil {
			slog.Error("save progress", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "progress": a.store.Snapshot()})
}

func (a *API) check(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if !a.course.hasCheck(req.ID) {
		writeError(w, http.StatusNotFound, "пункт не найден")
		return
	}
	if err := a.store.SetCheck(req.ID, req.Done); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"progress": a.store.Snapshot()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
