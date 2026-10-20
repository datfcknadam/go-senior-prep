//go:build !solution

package errs

import "context"

// NotFoundError — сущность не найдена.
// Должна сравниваться с ErrNotFound через errors.Is.
type NotFoundError struct {
	Resource string
	ID       int
}

func (e *NotFoundError) Error() string {
	panic("TODO")
}

type Service struct {
	Repo UserRepo
}

// GetUser валидирует id, ходит в репозиторий и оборачивает ошибки:
// невалидный id → ErrValidation, sql.ErrNoRows → *NotFoundError, остальное — с контекстом.
func (s *Service) GetUser(ctx context.Context, id int) (User, error) {
	panic("TODO")
}

// IsRetryable сообщает, имеет ли смысл повторить операцию.
func IsRetryable(err error) bool {
	panic("TODO")
}
