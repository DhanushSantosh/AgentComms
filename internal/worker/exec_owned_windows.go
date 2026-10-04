//go:build windows

package worker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ownedExec struct{ job windows.Handle }

func prepareOwnedExec(command *exec.Cmd) (*ownedExec, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure provider job: %w", err)
	}
	attr := syscall.SysProcAttr{}
	if command.SysProcAttr != nil {
		attr = *command.SysProcAttr
	}
	attr.CreationFlags |= windows.CREATE_SUSPENDED
	command.SysProcAttr = &attr
	return &ownedExec{job: job}, nil
}

func (p *ownedExec) started(ctx context.Context, command *exec.Cmd) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return fmt.Errorf("open suspended provider: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(p.job, process); err != nil {
		return fmt.Errorf("assign provider job: %w", err)
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("inspect suspended provider thread: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(command.Process.Pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("open suspended provider thread: %w", err)
		}
		if err := ctx.Err(); err != nil {
			windows.CloseHandle(thread)
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return fmt.Errorf("resume owned provider: %w", err)
		}
		return nil
	}
	return errors.New("suspended provider thread not found")
}

func (p *ownedExec) terminate() error { return windows.TerminateJobObject(p.job, 1) }
func (p *ownedExec) release()         { windows.CloseHandle(p.job) }
