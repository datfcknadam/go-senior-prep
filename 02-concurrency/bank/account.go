package bank

import (
	"errors"
	"sync"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrSameAccount       = errors.New("same account")
)

type Account struct {
	ID      int64
	mu      sync.Mutex
	balance int64
}

func NewAccount(id, balance int64) *Account {
	return &Account{ID: id, balance: balance}
}

func (a *Account) Balance() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.balance
}
