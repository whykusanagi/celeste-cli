//go:build windows

// Package proctree runs a command as the root of its own process tree and
// kills the whole tree: hooks and the shell tools share it, so a timeout
// takes down whatever the command started as well.
package proctree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// taskkillTimeout bounds the fallback kill, so a slow taskkill can't hold
// up a cancelled call.
const taskkillTimeout = 3 * time.Second

// Start starts cmd as the root of its own process tree and makes cancelling
// its context kill the whole tree. Use it instead of cmd.Start; it keeps
// any SysProcAttr fields already set (callers may set CmdLine).
//
// The process starts suspended, joins a new Job Object and is then resumed,
// so everything it starts is in the job before it can run: Kill ends the
// tree with one TerminateJobObject, which neither waits on another program
// nor depends on parent links that end when a parent exits. If the process
// can't join a job (an old Windows or a parent job that forbids nesting),
// Kill falls back to a time-bounded taskkill /T.
func Start(cmd *exec.Cmd) error {
	t := &tree{}
	cmd.Cancel = func() error { return t.kill(cmd) }
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	// Held until the process runs: a cancel that lands meanwhile waits a
	// moment and then finds the job.
	t.mu.Lock()
	if err := cmd.Start(); err != nil {
		t.mu.Unlock()
		return err
	}
	pid := uint32(cmd.Process.Pid)
	job, jobErr := newJob(pid)
	t.job = job
	resumeErr := resume(pid)
	t.mu.Unlock()
	if job != 0 {
		// The handle lives as long as cmd (through cmd.Cancel), so Kill
		// works after Wait too. The job has no kill-on-close limit: closing
		// the handle leaves running a background process that let go of
		// the output, as on unix.
		runtime.AddCleanup(t, func(h windows.Handle) { _ = windows.CloseHandle(h) }, job)
	}
	if resumeErr != nil {
		_ = killRoot(cmd) // still suspended: it has started nothing
		_ = cmd.Wait()
		return fmt.Errorf("start %s: %w", cmd.Path, errors.Join(resumeErr, jobErr))
	}
	return nil
}

// Kill ends cmd's process tree. A command that never started, or has
// already exited, is not an error. cmd must have been started by Start;
// after Wait it still ends whatever the command left running in its job.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if cmd.Cancel != nil {
		return cmd.Cancel()
	}
	return killRoot(cmd)
}

// tree is one started command's Job Object.
type tree struct {
	mu  sync.Mutex
	job windows.Handle // 0: no job, so kill falls back to taskkill /T
}

func (t *tree) kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != 0 {
		err := windows.TerminateJobObject(job, 1)
		runtime.KeepAlive(t) // its cleanup closes job
		if err != nil {
			return fmt.Errorf("terminate job: %w", err)
		}
		return nil
	}
	// Signal 0 only asks whether Wait has released the process (the only
	// state it reports on Windows). Until then Go holds a handle to it, so
	// its pid can't be reused; afterwards taskkill /T on that pid could end
	// an unrelated tree.
	if errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskkillTimeout)
	defer cancel()
	taskkill := "taskkill"
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		taskkill = filepath.Join(systemRoot, "System32", "taskkill.exe")
	}
	_ = exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	return killRoot(cmd)
}

func killRoot(cmd *exec.Cmd) error {
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// newJob puts process pid in a new Job Object and returns the job, or 0 and
// why it couldn't.
func newJob(pid uint32) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job: %w", err)
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("assign to job: %w", err)
	}
	return job, nil
}

// resume resumes the threads of process pid, which was created suspended
// (os.StartProcess closes the main thread's handle, so it is found by id).
func resume(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("thread snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := 0
	for err = windows.Thread32First(snap, &entry); err == nil; err = windows.Thread32Next(snap, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("open thread: %w", err)
		}
		_, err = windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
		if err != nil {
			return fmt.Errorf("resume thread: %w", err)
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("thread walk: %w", err)
	}
	if resumed == 0 {
		return errors.New("no thread to resume")
	}
	return nil
}
