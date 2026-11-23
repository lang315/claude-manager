package account

import "strings"

// cmdUnsafePath reports a claude path cmd would act on: the outer `cmd /c start`
// parses it outside quotes, so quotes, %, line ends and & | < > ^ all bite.
func cmdUnsafePath(p string) bool { return strings.ContainsAny(p, "\"%\r\n&|<>^") }
