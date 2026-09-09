// Package dashboard renders the audit/fix runner without owning hardening logic.
package dashboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"hardener/internal/config"
	"hardener/internal/executor"
	"hardener/internal/ui"
)

type Options struct {
	Mode          string
	System        string
	SecurityLevel string
	KeepSudoAlive bool
}

type Work func(observer executor.Observer, stop func() bool) error

var ErrStopped = errors.New("run stopped by user; completed checks were retained")

func Available() bool {
	return os.Getenv("TERM") != "dumb" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// Run owns the terminal until the user closes the completed dashboard. Quit
// during execution requests a boundary stop instead of killing an active fix.
func Run(options Options, suites []config.TestSuite, work Work) error {
	if !Available() {
		return errors.New("dashboard requires an interactive terminal")
	}
	ready, exited, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var stopping atomic.Bool
	requestStop := func() { stopping.Store(true) }
	m := newModel(options, suites, ready, requestStop)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithFPS(20), tea.WithoutSignalHandler())

	restore := ui.SetLogSink(func(entry ui.LogEntry) {
		// Structured runner events already render these statuses accurately.
		switch entry.Level {
		case "pass", "fail", "skip", "suite", "summary":
			return
		}
		p.Send(entry)
	})
	defer restore()

	var workErr error
	go func() {
		defer close(workerDone)
		select {
		case <-ready:
		case <-exited:
			return
		}
		// Bubble Tea can fail before startup. Do not start work after that.
		select {
		case <-exited:
			return
		default:
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					workErr = fmt.Errorf("execution panicked: %v", r)
				}
			}()
			workErr = work(func(event executor.RunEvent) { p.Send(event) }, stopping.Load)
		}()
		p.Send(finishedMsg{err: workErr, stopped: stopping.Load()})
	}()

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	go func() {
		for {
			select {
			case <-interrupts:
				p.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
			case <-exited:
				return
			}
		}
	}()
	if options.KeepSudoAlive {
		go func() {
			ticker := time.NewTicker(40 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-workerDone:
					return
				case <-exited:
					return
				case <-ticker.C:
					// Never prompt for credentials while the terminal is in raw mode.
					if err := exec.Command("sudo", "-n", "-v").Run(); err != nil {
						requestStop()
						p.Send(ui.LogEntry{Level: "error", Message: "Sudo credential refresh failed; stopping after the current check."})
						return
					}
				}
			}
		}()
	}

	_, terminalErr := p.Run()
	close(exited)
	// Even a renderer error must not leave a background fix orphaned.
	wasStopped := stopping.Load()
	requestStop()
	<-workerDone
	if terminalErr != nil {
		return terminalErr
	}
	if workErr != nil {
		return workErr
	}
	if wasStopped {
		return ErrStopped
	}
	return nil
}
