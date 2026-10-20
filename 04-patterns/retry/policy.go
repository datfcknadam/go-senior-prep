package retry

import (
	"context"
	"time"
)

type Policy struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
	// Sleep ждёт d или отмену ctx. nil — настоящее ожидание. Подменяется в тестах.
	Sleep func(ctx context.Context, d time.Duration) error
}
