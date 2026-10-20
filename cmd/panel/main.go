// Команда panel — локальная веб-панель для прохождения курса:
// теория, запуск тестов задач, чек-листы и прогресс.
//
//	go run ./cmd/panel            # http://localhost:8080
//	go run ./cmd/panel -addr :9000
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "адрес HTTP-сервера")
	root := flag.String("root", "", "корень репозитория (по умолчанию ищется go.mod вверх от текущей папки)")
	flag.Parse()

	if err := run(*addr, *root); err != nil {
		slog.Error("panel", "err", err)
		os.Exit(1)
	}
}

func run(addr, root string) error {
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	course, err := scanCourse(root)
	if err != nil {
		return fmt.Errorf("scan course: %w", err)
	}
	store, err := openStore(filepath.Join(root, ".progress.json"))
	if err != nil {
		return fmt.Errorf("open progress: %w", err)
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	api := &API{root: root, course: course, store: store, runner: newRunner(root, 2), play: newPlayground(root, 2)}

	mux := http.NewServeMux()
	api.register(mux)
	mux.Handle("GET /", http.FileServerFS(static))

	srv := &http.Server{
		Addr:              addr,
		Handler:           localOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("порт занят? попробуй -addr 127.0.0.1:8081: %w", err)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	slog.Info("панель запущена", "url", "http://"+displayAddr(ln.Addr().String()), "root", root)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// findRoot поднимается от текущей папки до каталога с go.mod.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod не найден: запусти панель из репозитория или укажи -root")
		}
		dir = parent
	}
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
