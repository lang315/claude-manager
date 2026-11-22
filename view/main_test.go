package view

import (
	"os"
	"testing"

	"github.com/lang315/claude-manager/internal/fixture"
)

func TestMain(m *testing.M) {
	fixture.RunFakeClaude() // the sign-in tests' claude is this binary
	os.Exit(m.Run())
}
