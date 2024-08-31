//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package pawnd

import (
	"os"
	"syscall"
)

var SupportedSignals = map[string]os.Signal{
	"sigusr1": syscall.SIGUSR1,
	"sigusr2": syscall.SIGUSR2,
}
