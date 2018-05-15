package process

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

var (
	// ErrProcCommandEmpty is an error that occurs when calling FindProcess
	// for a Process and the Process's command is empty.
	ErrProcCommandEmpty = fmt.Errorf("error: process command is empty")

	// ErrProcNotRunning is an error that is returned when running a health check
	// for a process and the process is not running.
	ErrProcNotRunning = fmt.Errorf("error: process is not running")

	// ErrProcNotInTty is an error that occurs when trying to open a Process's
	// tty but the Process does not have it's tty value set.
	ErrProcNotInTty = fmt.Errorf("process is not in a tty")

	// ErrInvalidNumber is an error that occurs when the number scanned in
	// whilst searching for a ProcessByName is less than 0.
	ErrInvalidNumber = fmt.Errorf("please enter a valid number")
)

// Process describes a unix process.
//
// The Process's Pid and the methods Kill(), Release(), Signal()
// and Wait() are implemented by composition with os.Process.
type Process struct {
	*os.Process
	Tty  string
	Cwd  string
	Cmd  string
	Args []string
}

// String returns all of the process's relevant information as a string.
func (p *Process) String() string {
	return fmt.Sprintf("[Pid]: %d\n"+
		"[Command]: %s\n"+
		"[Args]: %s\n"+
		"[Cwd]: %v\n"+
		"[Tty]: %s\n",
		p.Pid,
		p.Cmd,
		strings.Join(p.Args, ", "),
		p.Cwd,
		p.Tty,
	)
}

// HealthCheck signals the process to see if it's still running.
func (p *Process) HealthCheck() error {
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return ErrProcNotRunning
	}
	return nil
}

// Start starts a process and notifies on the notify channel
// when the process has been started. It uses stdin, stdout and
// stderr for the command's stdin, stdout and stderr respectively.
//
// If the notify channel is nil, just return normally so the call doesn't block.
func (p *Process) Start(detach bool, stdin io.Reader, stdout, stderr io.Writer,
	notify chan<- struct{}) error {
	// Create a new command to start the process with.
	c := exec.Command(p.Cmd, p.Args...)
	c.Stdin = stdin
	c.Stdout = stdout
	c.Stderr = stderr

	if p.InTty() {
		// Start the process in a different process group if detach is set to true.
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: detach}
	} else {
		// If process didn't start in a tty and detach is true, disconnect
		// process from any tty.
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: detach}
	}

	// Start the command.
	if err := c.Start(); err != nil {
		return err
	}

	// Notify that the process has started if notify isn't nil.
	if notify != nil {
		notify <- struct{}{}
	}

	// Wait for the command to finish.
	return c.Wait()
}

// StartTty requires sudo to work.
//
// StartTty starts a process in a tty and notifies on the notify channel
// when the process has been started.
//
// If the notify channel is nil, just return normally so the call doesn't block.
//
// The notify channel is here for consistency with the notify channel from
// the Start method.
func (p *Process) StartTty(ttyFd uintptr, notify chan<- struct{}) error {
	// Append a new line character to the full command so the command
	// actually executes.
	fullCommandNL := p.FullCommand() + "\n"

	// Write each byte from fullCommandNL to the tty instance.
	var eno syscall.Errno
	for _, b := range fullCommandNL {
		_, _, eno = syscall.Syscall(syscall.SYS_IOCTL,
			ttyFd,
			syscall.TIOCSTI,
			uintptr(unsafe.Pointer(&b)),
		)
		if eno != 0 {
			return error(eno)
		}
	}

	// Get the new PID of the restarted process.
	if err := p.FindProcess(); err != nil {
		return err
	}

	// Notify that the process has started if notify isn't nil.
	if notify != nil {
		notify <- struct{}{}
	}

	return nil
}

// FindProcess finds and then sets a Process's process based
// on it's command, it's command's arguments and it's tty.
func (p *Process) FindProcess() error {
	if p.Process == nil {
		p.Process = &os.Process{}
	}

	if p.Cmd == "" {
		return ErrProcCommandEmpty
	}

	ps, err := exec.Command("ps", "-e").Output()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(ps))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, p.Cmd) && strings.Contains(line, p.Tty) {
			p.Pid, err = strconv.Atoi(strings.TrimSpace(
				strings.FieldsFunc(line, unicode.IsSpace)[0]),
			)
			if err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Reset p.Process to the new process found from the new pid.
	p.Process, err = os.FindProcess(p.Pid)
	return err
}

// FullCommand returns a string containing the process's
// cmd and any args that it has joined to it by a space.
//
// If there are no args, FullCommand returns just the cmd.
func (p *Process) FullCommand() string {
	if len(p.Args) == 0 {
		return p.Cmd
	}
	return fmt.Sprintf("%s %s", p.Cmd, strings.Join(p.Args, " "))
}

// InTty returns a true or false depending if p.Tty is ?? or
// a value such as ttys001.
func (p *Process) InTty() bool {
	return p.Tty[0] != "?"[0]
}

// OpenTty returns an opened file handle to the tty of the process.
func (p *Process) OpenTty() (*os.File, error) {
	if !p.InTty() {
		return nil, ErrProcNotInTty
	}
	return os.Open("/dev/" + p.Tty)
}

// Chdir changes the current working directory to the processes cwd.
func (p *Process) Chdir() error {
	return os.Chdir(p.Cwd)
}

// Find by name takes in a name and through a process of elimination by
// prompting the user to select the correct process from a list, finds
// and returns a process by it's name.
//
// FindByName writes the list of names to the specified stdout and then scans
// the number for choosing the correct name from the specified stdin.
func FindByName(stdout io.Writer, stdin io.Reader, name string) (*Process, error) {
	psOutput, err := exec.Command("ps", "-e").Output()
	if err != nil {
		return nil, err
	}
	lowercaseOutput := bytes.ToLower(psOutput)

	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(lowercaseOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, name) {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Display a list of all the found names.
	for i, name := range names {
		fmt.Printf("%d: %s\n", i, name)
	}

	procNumber := -1
	fmt.Fprintln(stdout, "\nWhich number above represents the correct process (enter the number):")
	fmt.Fscanf(stdin, "%d", &procNumber)

	if procNumber < 0 {
		return nil, ErrInvalidNumber
	}

	pid, err := strconv.Atoi(strings.TrimSpace(
		strings.FieldsFunc(names[procNumber], unicode.IsSpace)[0]),
	)
	if err != nil {
		return nil, err
	}

	return FindByPid(pid)
}

// FindByPid finds and returns a process by it's pid.
func FindByPid(pid int) (*Process, error) {
	proc := new(Process)

	var err error
	proc.Process, err = os.FindProcess(pid)
	if err != nil {
		return nil, err
	}

	pidStr := strconv.Itoa(proc.Pid)

	// Get the tty= and comm= result from ps. Extract the tty of the process from
	// the tty= result and use the comm= result to compare to the command= result
	// below, to extract the process's command args.
	//
	// ps -o tty=,comm= -p $PID
	pidCmd, err := exec.Command("ps", "-o", "tty=,comm=", pidStr).Output()
	if err != nil {
		return nil, err
	}

	// Split the tty and command parts from the result of the above ps command.
	psfields := strings.FieldsFunc(string(pidCmd), unicode.IsSpace)

	// Get the tty of the process.
	proc.Tty = psfields[0]

	// Get the proc's command.
	proc.Cmd = strings.Join(psfields[1:], " ")

	// Extract process's args.
	//
	// Get the ps command= string result.
	pidCommandEq, err := exec.Command("ps", "-o", "command=", pidStr).Output()
	if err != nil {
		return nil, err
	}

	// Split the command= string after the comm= string.
	split := strings.SplitAfter(string(pidCommandEq), proc.Cmd)

	// Set the process's args.
	proc.Args = strings.FieldsFunc(split[1], unicode.IsSpace)

	// Find folder of the process (cwd).
	//
	// lsof -p $PID
	lsofOutput, err := exec.Command("lsof", "-p", pidStr).Output()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(lsofOutput))
	for scanner.Scan() {
		words := strings.FieldsFunc(scanner.Text(), unicode.IsSpace)
		if words[3] == "cwd" {
			proc.Cwd = strings.TrimSpace(strings.Join(words[8:], " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return proc, nil
}
