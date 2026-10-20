package bank

import (
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestTransferBasic(t *testing.T) {
	a, b := NewAccount(1, 100), NewAccount(2, 0)
	if err := Transfer(a, b, 30); err != nil {
		t.Fatal(err)
	}
	if a.Balance() != 70 || b.Balance() != 30 {
		t.Fatalf("balances %d %d", a.Balance(), b.Balance())
	}
	if err := Transfer(b, a, 31); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("got %v", err)
	}
	if a.Balance() != 70 || b.Balance() != 30 {
		t.Fatal("при ошибке балансы не должны меняться")
	}
	if err := Transfer(a, a, 1); !errors.Is(err, ErrSameAccount) {
		t.Fatalf("got %v", err)
	}
}

func TestNoDeadlockAndConservation(t *testing.T) {
	accs := make([]*Account, 5)
	for i := range accs {
		accs[i] = NewAccount(int64(i), 1000)
	}

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				for range 2000 {
					from, to := accs[rand.IntN(len(accs))], accs[rand.IntN(len(accs))]
					_ = Transfer(from, to, rand.Int64N(50))
				}
			})
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("похоже на дедлок")
	}

	var total int64
	for _, a := range accs {
		if a.Balance() < 0 {
			t.Fatalf("отрицательный баланс у счёта %d", a.ID)
		}
		total += a.Balance()
	}
	if total != 5000 {
		t.Fatalf("сумма денег изменилась: %d", total)
	}
}
