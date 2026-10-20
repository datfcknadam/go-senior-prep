//go:build solution

package errs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type NotFoundError struct {
	Resource string
	ID       int
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %d not found", e.Resource, e.ID)
}

// Is позволяет errors.Is(err, ErrNotFound) находить любую *NotFoundError.
func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

type Service struct {
	Repo UserRepo
}

func (s *Service) GetUser(ctx context.Context, id int) (User, error) {
	if id <= 0 {
		return User{}, fmt.Errorf("get user: id must be positive, got %d: %w", id, ErrValidation)
	}
	u, err := s.Repo.GetByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, &NotFoundError{Resource: "user", ID: id}
	}
	if err != nil {
		return User{}, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

func IsRetryable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var t interface{ Temporary() bool }
	return errors.As(err, &t) && t.Temporary()
}
