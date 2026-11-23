package account

import "testing"

func TestSwapWithItself(t *testing.T) {
	b, _, _, _ := switchBook(t)
	if err := b.Swap(1, 1); err == nil {
		t.Error("swapped an account with itself")
	}
}
