package process

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

var pid int
var currentTty, cwd, cmd, fullCommand string
var args []string

func init() {
	pid = os.Getpid()

	ttyBytes, err := exec.Command("ps", "-o", "tty=", strconv.Itoa(pid)).Output()
	if err != nil {
		log.Fatalln(err)
	}
	currentTty = strings.TrimSpace(string(ttyBytes))

	cmd = os.Args[0]

	cwd, err = os.Getwd()
	if err != nil {
		log.Fatalln(err)
	}

	args = os.Args[1:]

	if len(args) == 0 {
		fullCommand = cmd
	} else {
		fullCommand = fmt.Sprintf("%s %s", cmd, strings.Join(args, " "))
	}
}

func TestFindByPid(t *testing.T) {
	proc, err := FindByPid(pid)
	if err != nil {
		log.Fatalln(err)
	}

	if proc.Tty != currentTty {
		t.Errorf("proc tty incorrect, expected %s found %s",
			currentTty, proc.Tty)
	}

	if proc.Cwd != cwd {
		t.Errorf("proc cwd incorrect, expected %s found %s",
			cwd, proc.Cwd)
	}

	for i, arg := range args {
		if proc.Args[i] != arg {
			t.Errorf("proc arg incorrect, expected %s found %s",
				arg, proc.Args[i])
		}
	}

	if proc.Cmd != cmd {
		t.Errorf("proc cmd incorrect, expected %s found %s",
			cmd, proc.Cmd)
	}
}

func TestHealthCheck(t *testing.T) {
	// Start a new process that sleeps for 5 seconds.
	sleepCmd := exec.Command("sleep", "5")
	if err := sleepCmd.Start(); err != nil {
		t.Error(err)
	}

	// Create a new Process from the sleepCmd's process.
	proc, err := FindByPid(sleepCmd.Process.Pid)
	if err != nil {
		t.Error(err)
	}

	// HealthCheck the process and make sure it's running.
	if err := proc.HealthCheck(); err != nil {
		t.Error("expected process to be running")
	}

	// Stop the process.
	if err := proc.Release(); err != nil {
		t.Error(err)
	}

	// HealthCheck the process again and make sure it's stopped running.
	if err := proc.HealthCheck(); err == nil {
		t.Error("expected process to be stopped")
	}
}

func TestFullCommand(t *testing.T) {
	proc, err := FindByPid(pid)
	if err != nil {
		t.Error(err)
	}

	if proc.FullCommand() != fullCommand {
		t.Errorf("proc full command incorrect, expected %s, found %s",
			fullCommand, proc.FullCommand())
	}
}

func TestFindProcess(t *testing.T) {
	proc := &Process{
		Cmd:  cmd,
		Args: args,
		Tty:  currentTty,
	}

	if err := proc.FindProcess(); err != nil {
		t.Error(err)
	}

	if proc.Pid != pid {
		t.Errorf("proc pid is incorrect, expected %d, found %d", pid, proc.Pid)
	}
}
