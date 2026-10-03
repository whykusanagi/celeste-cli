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
	"weak"

	"golang.org/x/sys/windows"
)

// taskkillTimeout bounds the fallback kill, so a slow taskkill can't hold
// up a cancelled call.
const taskkillTimeout = 3 * time.Second

// StartSession is Start: a Windows command never shares a Unix
// controlling terminal.
func StartSession(cmd *exec.Cmd) error { return Start(cmd) }

// Start starts cmd as the root of its own process tree and makes cancelling
// its context kill the whole tree. Use it instead of cmd.Start; it keeps
// any SysProcAttr fields already set (callers may set CmdLine).
// cmd must come from exec.CommandContext.
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
		// Release closes the handle once the caller is done with cmd; the
		// cleanup is the backstop for a caller that never calls it. The
		// tree holds no reference to cmd, so the map entry doesn't keep
		// cmd alive.
		key := weak.Make(cmd)
		trees.Store(key, t)
		runtime.AddCleanup(cmd, func(k weak.Pointer[exec.Cmd]) {
			if v, ok := trees.LoadAndDelete(k); ok {
				v.(*tree).release()
			}
		}, key)
	}
	if resumeErr != nil {
		_ = killRoot(cmd) // still suspended: it has started nothing
		_ = cmd.Wait()
		Release(cmd)
		return fmt.Errorf("start %s: %w", cmd.Path, errors.Join(resumeErr, jobErr))
	}
	return nil
}

// Kill ends cmd's process tree. A command that never started, or has
// already exited, is not an error. cmd must have been started by Start;
// after Wait (until Release) it still ends whatever the command left
// running in its job.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if cmd.Cancel != nil {
		return cmd.Cancel()
	}
	return killRoot(cmd)
}

// Release closes cmd's Job Object once the caller is done with cmd (after
// Wait, and after any Kill). Processes still in the job keep running. It
// is idempotent and safe against a concurrent Kill or cancel, which become
// no-ops once it has run.
func Release(cmd *exec.Cmd) {
	if v, ok := trees.LoadAndDelete(weak.Make(cmd)); ok {
		v.(*tree).release()
	}
}

// trees maps each started command still holding a Job Object to its tree.
var trees sync.Map // weak.Pointer[exec.Cmd] -> *tree

// tree is one started command's Job Object.
type tree struct {
	mu       sync.Mutex
	job      windows.Handle // 0: no job, so kill falls back to taskkill /T
	released bool
}

func (t *tree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
	t.released = true
}

func (t *tree) kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	t.mu.Lock()
	if t.released {
		t.mu.Unlock()
		return nil
	}
	if t.job != 0 {
		// Under the lock, so Release can't close the handle mid-call.
		err := windows.TerminateJobObject(t.job, 1)
		t.mu.Unlock()
		if err != nil {
			return fmt.Errorf("terminate job: %w", err)
		}
		return nil
	}
	t.mu.Unlock()
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
	// BREAKAWAY_OK lets a program that explicitly asks for
	// CREATE_BREAKAWAY_FROM_JOB (installers, some MSBuild tooling) start
	// outside the job instead of failing; such a process is no longer in
	// the tree Kill ends. SILENT_BREAKAWAY_OK is not set: every other child
	// stays in the job. A job that can't take the limit is still used, only
	// stricter.
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
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
	var last error
	for err = windows.Thread32First(snap, &entry); err == nil; err = windows.Thread32Next(snap, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		// A thread that can't be opened or resumed is skipped (and named
		// if nothing could be resumed).
		th, terr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if terr != nil {
			last = fmt.Errorf("open thread %d: %w", entry.ThreadID, terr)
			continue
		}
		_, terr = windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
		if terr != nil {
			last = fmt.Errorf("resume thread %d: %w", entry.ThreadID, terr)
			continue
		}
		resumed++
	}
	if resumed > 0 {
		return nil
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("thread walk: %w", err)
	}
	if last != nil {
		return last
	}
	return errors.New("no thread to resume")
}
