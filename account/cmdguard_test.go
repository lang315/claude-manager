package account

import "testing"

func TestCmdUnsafePath(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Users\me\claude.exe`: false, `C:\Program Files\claude.cmd`: false,
		`C:\a&b\claude.exe`: true, `C:\a|b`: true, `C:\a<b`: true, `C:\a>b`: true, `C:\a^b`: true,
		`C:\a"b`: true, `C:\a%b`: true, "C:\\a\nb": true,
	} {
		if got := cmdUnsafePath(p); got != want {
			t.Errorf("cmdUnsafePath(%q) = %v", p, got)
		}
	}
}
