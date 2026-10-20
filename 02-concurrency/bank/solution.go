//go:build solution

package bank

func Transfer(from, to *Account, amount int64) error {
	if from == to {
		return ErrSameAccount
	}
	// Глобальный порядок захвата по ID ломает циклическое ожидание (условие Коффмана).
	first, second := from, to
	if second.ID < first.ID {
		first, second = second, first
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	second.mu.Lock()
	defer second.mu.Unlock()

	if from.balance < amount {
		return ErrInsufficientFunds
	}
	from.balance -= amount
	to.balance += amount
	return nil
}
