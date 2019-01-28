package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

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
	pawnfile := filepath.Join(testdir, "Pawnfile")

	opts := util.NewOptions()
	opts.Set("configuration-file", pawnfile)

	opSleep := func(d time.Duration) func() error {
		return func() error {
			time.Sleep(d)
			return nil
		}
	}

	opTerminate := func() error {
		return nil
	}

	opPawnfile := func(contents string) func() error {
		return func() error {
			return ioutil.WriteFile(pawnfile, []byte(contents), 0666)
		}
	}

	type opfunc func() error

	tests := []struct {
		name            string
		preops          []opfunc
		ops             []func() error
		ExpectedErrorRe string
	}{
		{"No pawnfile", nil, nil, "Could not load config.*no such file"},
		// {"Empty pawnfile", []opfunc{
		// 	opPawnfile(""),
		// }, nil, ""},
		{"Parse: Section type missing", []opfunc{
			opPawnfile(`[something]
`),
		}, nil, "should have exactly one of"},
		{"Parse: Section type missing with contents", []opfunc{
			opPawnfile(`[something]
contents=but missing
`),
		}, nil, "should have exactly one of"},
		{"Parse: Empty file hysteresis", []opfunc{
			opPawnfile(`[fp]
file=abc
hysteresis=
`),
		}, nil, "Duration"},
		{"Parse: Invalid file hysteresis", []opfunc{
			opPawnfile(`[fp]
file=abc
hysteresis=c
`),
		}, nil, "Duration"},
		{"Parse: Invalid file hysteresis 2", []opfunc{
			opPawnfile(`[fp]
file=abc
hysteresis=10
`),
		}, nil, "Duration"},
		{"Parse: Invalid exec cooldown", []opfunc{
			opPawnfile(`[fp]
exec=false
cooldown
`),
		}, nil, "Duration"},
		{"Parse: Invalid exec cooldown 2", []opfunc{
			opPawnfile(`[fp]
exec=false
cooldown=1
`),
		}, nil, "Duration"},
		{"Parse: Invalid exec timeout", []opfunc{
			opPawnfile(`[fp]
exec=false
timeout="something"
`),
		}, nil, "Duration"},
		{"Parse: Invalid exec timeout 2", []opfunc{
			opPawnfile(`[fp]
exec=false
timeout==
`),
		}, nil, "Duration"},
		{"Parse: Invalid script", []opfunc{
			opPawnfile(`[fp]
script=if false; do
`),
		}, nil, "Script parse error"},
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

			wg := sync.WaitGroup{}
			wg.Add(1)

			var opErr error

			go func() {
				opSleep(time.Millisecond * 10)()
				for i := range tt.ops {
					err := tt.ops[i]()
					if err != nil {
						opErr = fmt.Errorf("Op %d failed: %v\n  op: %v",
							i, err, spew.Sdump(tt.ops[i]))
						break
					}
				}
				opTerminate()
				wg.Done()
			}()
			err = Main(opts)
			if (err != nil) != (tt.ExpectedErrorRe != "") {
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

			wg.Wait()
			if opErr != nil {
				t.Fatalf("%v", opErr)
			}
		})
	}
}
