package pawnd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/davecgh/go-spew/spew"
	util "github.com/kopoli/go-util"
)

func Test_pawndRunning(t *testing.T) {
	buf := &bytes.Buffer{}

	deps = defaultDeps
	deps.FailSafeExit = func() {
		panic("Failsafe exit!")
	}
	deps.NewTerminalStdout = func() io.Writer {
		return buf
	}

	testdir := "integration-test"

	opts := util.NewOptions()
	opts.Set("configuration-file", filepath.Join(testdir, "Pawnfile"))

	tests := []struct {
		name            string
		preops          []func() error
		ops             []func() bool
		ExpectedErrorRe string
	}{
		{"No pawnfile", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := os.MkdirAll(testdir, 0755)
			if err != nil {
				t.Fatal("Could not create test directory:", err)
			}

			defer func() {
				err := os.RemoveAll(testdir)
				if err != nil {
					t.Error("Could not remove test directory:", err)
				}
			}()

			buf.Reset()

			for i := range tt.preops {
				err = tt.preops[i]()
				if err != nil {
					t.Fatalf("Pre-op %d failed: %v\n  op: %v",
						i, err, spew.Sdump(tt.preops[i]))
				}
			}

			err = Main(opts)
			if (err != nil) == (tt.ExpectedErrorRe != "") {
				t.Fatalf("Got error: %t expected error: %t\n",
					err != nil, tt.ExpectedErrorRe != "")
			}

			if err != nil {
				re := regexp.MustCompile(tt.ExpectedErrorRe)
				if !re.MatchString(err.Error()) {
					t.Fatalf("Error '%v' didn't match regexp %s",
						err, tt.ExpectedErrorRe)
				}
			}
		})
	}
}
