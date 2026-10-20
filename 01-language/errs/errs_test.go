package errs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeRepo struct {
	user User
	err  error
}

func (f fakeRepo) GetByID(context.Context, int) (User, error) { return f.user, f.err }

type tempErr struct{ temp bool }

func (e tempErr) Error() string   { return "temp" }
func (e tempErr) Temporary() bool { return e.temp }

func TestGetUser(t *testing.T) {
	ctx := context.Background()

	t.Run("ok", func(t *testing.T) {
		s := &Service{Repo: fakeRepo{user: User{ID: 1, Name: "a"}}}
		u, err := s.GetUser(ctx, 1)
		if err != nil || u.Name != "a" {
			t.Fatalf("got %v, %v", u, err)
		}
	})

	t.Run("validation", func(t *testing.T) {
		s := &Service{Repo: fakeRepo{}}
		_, err := s.GetUser(ctx, 0)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("want ErrValidation, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		s := &Service{Repo: fakeRepo{err: fmt.Errorf("query: %w", sql.ErrNoRows)}}
		_, err := s.GetUser(ctx, 42)
		var nf *NotFoundError
		if !errors.As(err, &nf) || nf.ID != 42 || nf.Resource != "user" {
			t.Fatalf("want *NotFoundError{user 42}, got %#v", err)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Error("errors.Is(err, ErrNotFound) должен быть true")
		}
		if nf.Error() == "" {
			t.Error("пустое сообщение ошибки")
		}
	})

	t.Run("wrapped", func(t *testing.T) {
		base := errors.New("connection reset")
		s := &Service{Repo: fakeRepo{err: base}}
		_, err := s.GetUser(ctx, 42)
		if !errors.Is(err, base) {
			t.Fatal("исходная ошибка должна быть доступна через errors.Is")
		}
		if !strings.Contains(err.Error(), "42") {
			t.Errorf("в сообщении нет контекста: %q", err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("обычная ошибка не должна быть ErrNotFound")
		}
	})
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("x"), false},
		{context.DeadlineExceeded, true},
		{fmt.Errorf("call: %w", context.DeadlineExceeded), true},
		{context.Canceled, false},
		{fmt.Errorf("a: %w", tempErr{temp: true}), true},
		{tempErr{temp: false}, false},
		{errors.Join(errors.New("x"), tempErr{temp: true}), true},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
