package pawnd

// Contains dependencies that can be overridden during tests

import (
	"fmt"
	"os"
)

var FailSafeExit = func() {
	fmt.Fprintf(os.Stderr, "Error: Failsafe exit triggered\n")
	os.Exit(2)
}
