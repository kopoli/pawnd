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

	// type sigchan chan<-os.Signal
	sigchans := map[os.Signal]chan<-os.Signal{
		os.Interrupt: nil,
	}

	deps.SignalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
		for i := range sig {
			sigchans[sig[i]] = c
		}
	}

	deps.SignalReset = func(sig ...os.Signal) {
		for i := range sig {
			sigchans[sig[i]] = nil
		}

	}

	testdir := "integration-test"
	pawnfile := filepath.Join(testdir, "Pawnfile")

	opts := util.NewOptions()
	opts.Set("configuration-file", pawnfile)

	type opfunc func() error

	opSleep := func(d time.Duration) func() error {
		return func() error {
			time.Sleep(d)
			return nil
		}
	}

	opTerminate := func() error {
		sigchans[os.Interrupt] <- os.Interrupt
		return nil
	}

	opPawnfile := func(contents string) func() error {
		return func() error {
			return ioutil.WriteFile(pawnfile, []byte(contents), 0666)
		}
	}

	parsesOk := []opfunc{
		opSleep(time.Millisecond * 10),
		opTerminate,
	}

	type IntegrationTest struct {
		name            string
		preops          []opfunc
		ops             []opfunc
		ExpectedErrorRe string
	}

	PawnfileOk := func(name string, contents string) IntegrationTest {
		return IntegrationTest{
			name,
			[]opfunc{
				opPawnfile(contents),
			},
			parsesOk,
			"",
		}
	}
	PawnfileError := func(name string, contents string, errorRe string) IntegrationTest {
		return IntegrationTest{
			name,
			[]opfunc{
				opPawnfile(contents),
			},
			nil,
			errorRe,
		}
	}

	tests := []IntegrationTest{
		{"No pawnfile", nil, nil, "Could not load config.*no such file"},
		PawnfileOk("Empty pawnfile, parses ok", ""),
		PawnfileError("Parse: Section type missing", `[something]`,
			"should have exactly one of"),
		PawnfileError("Parse: Section type missing with contents",
			`[something]
contents=but missing`,
			"should have exactly one of"),
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
		{"Parse: Proper file hysteresis", []opfunc{
			opPawnfile(`[fp]
file=abc
hysteresis=100ms
`),
		}, parsesOk, ""},
		{"Parse: Proper file hysteresis 2", []opfunc{
			opPawnfile(`[fp]
file=abc
hysteresis=2s
`),
		}, parsesOk, ""},
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
		{"Parse: Proper exec cooldown", []opfunc{
			opPawnfile(`[fp]
exec=false
cooldown=1s
`),
		}, parsesOk, ""},
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
		{"Parse: Proper exec timeout", []opfunc{
			opPawnfile(`[fp]
exec=false
timeout=2h30m10s
`),
		}, parsesOk, ""},
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
