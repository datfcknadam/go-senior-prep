package errs

import (
	"context"
	"errors"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation failed")
)

type User struct {
	ID   int
	Name string
}

// UserRepo — интерфейс объявлен на стороне потребителя.
type UserRepo interface {
	GetByID(ctx context.Context, id int) (User, error)
}
