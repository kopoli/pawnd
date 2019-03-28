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
		PrintGoroutines()
		panic("Failsafe exit!")
	}
	deps.NewTerminalStdout = func() io.Writer {
		return buf
	}

	// type sigchan chan<-os.Signal
	sigchans := map[os.Signal]chan<- os.Signal{
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

	type opfunc func() error

	opSleep := func(d time.Duration) func() error {
		return func() error {
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
		sigchans[os.Interrupt] <- os.Interrupt
		return nil
	}

	opFile := func(name, contents string) func() error {
		name = filepath.Join(testdir, name)
		return func() error {
			return ioutil.WriteFile(name, []byte(contents), 0666)
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
			fmt.Println(buf.String())
			return nil
		}
	}
	_ = opPrintOutput

	opSetOpt := func(key string, value string) func() error {
		return func() error {
			opts.Set(key, value)
			return nil
		}
	}

	opSetVerbose := opSetOpt("verbose", "t")

	opExpectOutput := func(expectRe string) func() error {
		re := regexp.MustCompile(expectRe)
		return func() error {
			// Poll quickly if given string is in the output
			for i := 0; i < 1000; i++ {
				if re.MatchString(buf.String()) {
					return nil
				}
				time.Sleep(time.Millisecond * 2)
			}
			return fmt.Errorf("Could not find regexp: %s", expectRe)
		}
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
`, "Script parse error"),
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

		IntegrationTest{
			name: "If Pawnfile is updated, pawnd restarts",
			preops: []opfunc{
				opSetVerbose,
				opPawnfile("[something]\nscript=:"),
			},
			ops: []opfunc{
				opExpectOutput("something"),
				opPawnfile("[else happens]\nscript=! :"),
				opExpectOutput("File.*Pawnfile.*changed"),
				opPrintOutput(),
			},
			ExpectedErrorRe: "Main restarted",
		},

		PawnfileOps("Files to check for changes not found", `[filechange]
file=*.notfound
changed=changedtask
`,
			[]opfunc{
				opSetVerbose,
			},
			[]opfunc{
				opExpectOutput("Creating file watcher.*filechange.*failed"),
			}),

		PawnfileOps("Triggering with a file change", fmt.Sprintf(`[filechange]
file=%s/*.tmp
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
				opExpectOutput("Triggering changedtask"),
				opPrintOutput(),
			}),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			err := os.RemoveAll(testdir)
			if err != nil {
				t.Error("Could not remove test directory:", err)
			}

			err = os.MkdirAll(testdir, 0755)
			if err != nil {
				t.Fatal("Could not create test directory:", err)
			}

			opts = util.NewOptions()
			opts.Set("configuration-file", pawnfile)

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
				err := opSleep(time.Millisecond * 10)()
				if err != nil {
					opErr = fmt.Errorf("Internal error, sleep failed: %v\n", err)
				}
				for i := range tt.ops {
					fmt.Println("Running op", spew.Sdump(tt.ops[i]))
					err := tt.ops[i]()
					if err != nil {
						opErr = fmt.Errorf("Op %d failed: %v\n  op: %v",
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
			if opErr != nil {
				t.Fatalf("%v", opErr)
			}
		})
	}
}
