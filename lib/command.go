package pawnd

import (
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	util "github.com/kopoli/go-util"
)

type Process struct {
	Args []string

	// Channel that triggers the running of the command
	// Run <-chan Trigger

	CoolDown time.Duration

	Stdout io.Writer
	Stderr io.Writer

	IsDaemon bool
}

// type DoneChan chan<- *os.ProcessState

type process struct {
	Process
	Run chan Trigger
	// returns the exit code of the program
	Done chan<- int
}

type ProcessManager struct {
	procs []process
}

func (m *ProcessManager) Add(p Process, t chan Trigger) (err error) {
	if len(p.Args) == 0 || p.Args[0] == "" {
		err = util.E.New("Invalid arguments to run a command")
		return
	}

	if t == nil {
		err = util.E.New("Invalid trigger channel given")
		return
	}

	var pr process
	pr.Process = p
	pr.Run = t
	m.procs = append(m.procs, pr)

	go runner(pr)

	return
}

func runner(p process) {
	var cmd *exec.Cmd

	wg := sync.WaitGroup{}

	runCmd := func() {
		wg.Add(1)
		cmd = exec.Command(p.Args[0], p.Args[1:]...)
		cmd.Stdout = p.Stdout
		cmd.Stderr = p.Stderr
		err := cmd.Start()
		if err != nil {
			cmd = nil
			util.E.Print(err, "Starting command failed: ", p.Args)
			return
		}
		err = cmd.Wait()

		// Determine the exit code
		if err != nil {
			ret := 1
			if ee, ok := err.(*exec.ExitError); ok {
				if stat, ok := ee.Sys().(syscall.WaitStatus); ok {
					ret = stat.ExitStatus()
				}
			}
			p.Done <- ret
		} else {
			p.Done <- 0
		}
		wg.Done()
	}

	killCmd := func() (err error) {
		if cmd != nil && cmd.Process != nil {
			err = cmd.Process.Kill()
		}
		return
	}

	for {
		select {
		case _, ok := <-p.Run:
			// If the channel is closed the program should be terminated
			if !ok {
				_ = killCmd()
				return
			}

			if p.IsDaemon {
				_ = killCmd()
			} else {
				wg.Wait()
			}

			// Run the command
			go runCmd()
		}
	}

	return
}

// Trigger a run of all processes
func (m *ProcessManager) Trigger() {
	for _, p := range m.procs {
		p.Run <- Trigger{}
	}
}

// Terminate all processes
func (m *ProcessManager) KillAll() (err error) {
	return
}
