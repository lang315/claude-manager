package account

import (
	"fmt"
	"slices"
)

// Slot numbers, claude-swap's: each added account has a number from 1,
// for the command line (`switch 2`, `move`, `swap`).

// ensureSlots numbers each added account that has none with the smallest
// number free.
func (b *Book) ensureSlots() {
	for i := range b.added {
		if b.added[i].Slot != 0 {
			continue
		}
		for n := 1; ; n++ {
			if !slices.ContainsFunc(b.added, func(a Account) bool { return a.Slot == n }) {
				b.added[i].Slot = n
				break
			}
		}
	}
}

func (b *Book) atSlot(n int) int {
	return slices.IndexFunc(b.added, func(a Account) bool { return a.Slot == n })
}

// Move puts the account in slot from into slot to; an account already
// there takes slot from.
func (b *Book) Move(from, to int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	b.ensureSlots()
	i := b.atSlot(from)
	if i < 0 {
		return fmt.Errorf("No account is in slot %d.", from)
	}
	if to < 1 {
		return fmt.Errorf("Slots start at 1, not %d.", to)
	}
	if j := b.atSlot(to); j >= 0 {
		b.added[j].Slot = from
	}
	b.added[i].Slot = to
	return b.save()
}

// Swap exchanges the accounts in slots x and y.
func (b *Book) Swap(x, y int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.load(); err != nil {
		return err
	}
	b.ensureSlots()
	i, j := b.atSlot(x), b.atSlot(y)
	if i < 0 || j < 0 {
		return fmt.Errorf("Both slots must hold an account to swap them: %d and %d.", x, y)
	}
	b.added[i].Slot, b.added[j].Slot = y, x
	return b.save()
}
