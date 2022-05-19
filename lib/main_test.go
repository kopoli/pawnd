package pawnd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/kopoli/appkit"
	"go.uber.org/goleak"
)

type lockWriter struct {
	wr io.Writer
	sync.RWMutex
}

func (w *lockWriter) Write(p []byte) (n int, err error) {
	w.Lock()
	defer w.Unlock()

	return w.wr.Write(p)
}

func Test_pawndRunning(t *testing.T) {
	buf := &strings.Builder{}
	wr := &lockWriter{}
	wr.wr = buf

	deps = defaultDeps
	deps.FailSafeExit = func() {
		PrintGoroutines()
		panic("Failsafe exit!")
	}
	deps.NewTerminalStdout = func() io.Writer {
		return wr
	}

	// type sigchan chan<-os.Signal
	sigchans := map[os.Signal]chan<- os.Signal{
		os.Interrupt: nil,
	}
	sigMutex := sync.Mutex{}

	deps.SignalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
		sigMutex.Lock()
		defer sigMutex.Unlock()
		for i := range sig {
			sigchans[sig[i]] = c
		}
	}

	deps.SignalReset = func(sig ...os.Signal) {
		sigMutex.Lock()
		defer sigMutex.Unlock()
		for i := range sig {
			sigchans[sig[i]] = nil
		}
	}

	testdir := "integration-test"
	pawnfile := filepath.Join(testdir, "Pawnfile")

	opts := appkit.NewOptions()

	type opfunc func() error

	opSleep := func(d time.Duration) func() error {
		return func() error {
			runtime.Gosched()
			time.Sleep(d)
			return nil
		}
	}

	opPanic := func() error {
		fmt.Println("Sending panic")
		panic("Did not terminate")
	}
	_ = opPanic

	opTerminate := func() error {
		sigMutex.Lock()
		defer sigMutex.Unlock()
		sigchans[os.Interrupt] <- os.Interrupt
		return nil
	}

	opFile := func(name, contents string) func() error {
		name = filepath.Join(testdir, name)
		return func() error {
			fmt.Fprintln(os.Stderr, "TEST: creating contents to file", name)
			return os.WriteFile(name, []byte(contents), 0600)
		}
	}

	opPawnfile := func(contents string) func() error {
		return opFile("Pawnfile", contents)
	}

	parsesOk := []opfunc{
		opSleep(time.Millisecond * 10),
		opTerminate,
	}

	opPrintOutput := func() func() error {
		return func() error {
			wr.RLock()
			fmt.Println(buf.String())
			wr.RUnlock()
			return nil
		}
	}
	_ = opPrintOutput

	opYield := func() func() error {
		return func() error {
			for i := 0; i < 10; i++ {
				runtime.Gosched()
			}
			return nil
		}
	}
	_ = opYield

	opSetOpt := func(key string, value string) func() error {
		return func() error {
			opts.Set(key, value)
			return nil
		}
	}

	opSetVerbose := opSetOpt("verbose", "t")

	opExpectOutputParam := func(reg string, shouldFind bool) func() error {
		re := regexp.MustCompile(reg)
		return func() error {
			// Poll quickly if given string is in the output
			for i := 0; i < 10; i++ {
				wr.RLock()
				s := buf.String()
				wr.RUnlock()
				if re.MatchString(s) {
					_ = opPrintOutput()()
					if !shouldFind {
						return fmt.Errorf("found regexp: %s in string [%s]", reg, s)
					}
					return nil
				}
				runtime.Gosched()
				time.Sleep(time.Millisecond * 200)
			}
			_ = opPrintOutput()()
			if !shouldFind {
				return nil
			}
			return fmt.Errorf("could not find regexp: %s", reg)
		}
	}

	opExpectOutput := func(re string) func() error {
		return opExpectOutputParam(re, true)
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
	PawnfileOps := func(name string, contents string, preops []opfunc, ops []opfunc) IntegrationTest {
		preops = append(preops, opPawnfile(contents))
		ops = append(ops, parsesOk...)
		return IntegrationTest{
			name,
			preops,
			ops,
			"",
		}
	}

	tests := []IntegrationTest{
		{"No pawnfile", nil, nil, "Could not load config.*"},
		PawnfileOk("Empty pawnfile, parses ok", ""),
		PawnfileError("Parse: Section type missing", `[something]`,
			"should have exactly one of"),
		PawnfileError("Parse: Section type missing with contents",
			`[something]
contents=but missing`,
			"should have exactly one of"),
		PawnfileError("Parse: Empty file hysteresis", `[fp]
file=abc
hysteresis=
`, "Duration"),
		PawnfileError("Parse: Invalid file hysteresis", `[fp]
file=abc
hysteresis=c
`, "Duration"),
		PawnfileError("Parse: Invalid file hysteresis 2", `[fp]
file=abc
hysteresis=10
`, "Duration"),
		PawnfileOk("Parse: Proper file hysteresis", `[fp]
file=abc
hysteresis=100ms
`),
		PawnfileOk("Parse: Proper file hysteresis 2", `[fp]
file=abc
hysteresis=2s
`),
		PawnfileError("Parse: Invalid exec cooldown", `[fp]
exec=false
cooldown
`, "Duration"),
		PawnfileError("Parse: Invalid exec cooldown 2", `[fp]
exec=false
cooldown=1
`, "Duration"),
		PawnfileOk("Parse: Proper exec cooldown", `[fp]
exec=false
cooldown=1s
`),
		PawnfileError("Parse: Invalid exec timeout", `[fp]
exec=false
timeout="something"
`, "Duration"),
		PawnfileError("Parse: Invalid exec timeout 2", `[fp]
exec=false
timeout==
`, "Duration"),
		PawnfileOk("Parse: Proper exec timeout", `[fp]
exec=false
cooldown=2h30m10s
`),
		PawnfileError("Parse: Invalid script", `[fp]
script=if false; do
`, "script parse error"),
		PawnfileOk("Parse: Proper exec visible", `[fp]
exec=false
visible
`),
		PawnfileOk("Parse: Proper exec visible 2", `[fp]
exec=false
visible=
`),
		PawnfileOk("Parse: Proper exec visible 3", `[fp]
exec=false
visible=no
`),
		PawnfileOk("Parse: Proper exec visible 3", `[fp]
exec=false
visible=no
`),
		PawnfileOps("Running simple command", `[a]
exec=go run inttest.go jeejee
init
`,
			nil, []opfunc{
				opExpectOutput("inttest.*jeejee"),
			}),
		PawnfileOps("Running simple command with script", `[a]
script=go run inttest.go jeejee
init
`,
			nil, []opfunc{
				// Use opSleep to make the function used.
				opSleep(time.Millisecond),
				opExpectOutput("inttest.*jeejee"),
			}),
		PawnfileOps("Running proper cron", `[crontest]
cron=0 0 8,15 * * mon-fri
`,
			[]opfunc{
				opSetVerbose,
			}, []opfunc{
				opExpectOutput("Next: 20"),
			}),
		PawnfileOps("Triggering succeeding task", `[inttest]
init
script=:
succeeded=succtask

[succtask]
script=go run inttest.go piip
`,
			[]opfunc{
				opSetVerbose,
			},
			[]opfunc{
				opExpectOutput("inttest.*piip"),
			}),
		PawnfileOps("Triggering succeeding task with quotes", `[inttest]
init
script=" : "
succeeded=succtask

[succtask]
script=go run inttest.go piip2
`,
			[]opfunc{
				opSetVerbose,
			},
			[]opfunc{
				opExpectOutput("inttest.*piip2"),
			}),
		PawnfileOps("Triggering failing task", `[inttest]
init
script=! :
failed=failtask

[failtask]
script=go run inttest.go this failed
`,
			[]opfunc{
				opSetVerbose,
			},
			[]opfunc{
				opPrintOutput(),
				opExpectOutput("inttest.*this failed"),
			}),
		{
			name: "If Pawnfile is updated, pawnd restarts",
			preops: []opfunc{
				opSetVerbose,
				opPawnfile("[something]\nscript=:"),
			},
			ops: []opfunc{
				opExpectOutput("something"),
				opPawnfile("[else happens]\nscript=! :"),
				opExpectOutput("File.*Pawnfile.*changed"),
			},
			ExpectedErrorRe: "main restarted",
		},
		{
			name: "Explicit trigger of `pawnd-restart` restarts pawnd",
			preops: []opfunc{
				opSetVerbose,
				opPawnfile("[a]\ninit\nscript=:\nsucceeded=pawnd-restart"),
			},
			ops: []opfunc{
				opPrintOutput(),
			},
			ExpectedErrorRe: "main restarted",
		},

		PawnfileOps("Triggering with a file change", fmt.Sprintf(`[filechange]
file=%s/*.tmp
hysteresis=10ms
changed=changedtask

[changedtask]
script=:
`, testdir),
			[]opfunc{
				opSetVerbose,
				opFile("something.tmp", "contents here"),
			},
			[]opfunc{
				opExpectOutput("changedtask"),
				opFile("something.tmp", "changed contents"),
				opExpectOutput("Sending trigger to changedtask"),
				opPrintOutput(),
			}),

		PawnfileOps("Not triggering a change if all files are excluded", fmt.Sprintf(`[filechange]
file=%s/*.tmp !%s/*something*
hysteresis=300ms
changed=run

[run]
script=:
`, testdir, testdir),
			[]opfunc{
				opSetVerbose,
				opFile("something.tmp", "contents here"),
				opFile("other.tmp", "morecontents"),
			},
			[]opfunc{
				opExpectOutput("run"),
				opFile("something.tmp", "changed contents"),
				// wait for a while for the separate file
				// change to trigger
				opSleep(time.Millisecond * 100),
				opFile("other.tmp", "This should be notified"),
				// opYield(),
				opExpectOutputParam("Sending trigger to run", true),
			}),
	}
	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			err := os.RemoveAll(testdir)
			if err != nil {
				t.Error("Could not remove test directory:", err)
			}

			err = os.MkdirAll(testdir, 0755)
			if err != nil {
				t.Fatal("Could not create test directory:", err)
			}

			opts = appkit.NewOptions()
			opts.Set("configuration-file", pawnfile)
			opts.Set("pawnfile-hysteresis", "10")

			wr.Lock()
			buf.Reset()
			wr.Unlock()

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
				err := opSleep(time.Millisecond * 10)()
				if err != nil {
					opErr = fmt.Errorf("internal error, sleep failed: %v", err)
				}
				for i := range tt.ops {
					v := reflect.ValueOf(tt.ops[i])
					f := runtime.FuncForPC(v.Pointer())
					fname, line := f.FileLine(v.Pointer())
					fmt.Printf("Running op %v:%d\n", filepath.Base(fname), line)
					err := tt.ops[i]()
					if err != nil {
						opErr = fmt.Errorf("op %d failed: %v\n  op: %v",
							i, err, spew.Sdump(tt.ops[i]))
						_ = opTerminate()
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

			runtime.Gosched()
			if opErr != nil {
				t.Fatalf("Fatal opError: %v", opErr)
			}
		})
	}
}
