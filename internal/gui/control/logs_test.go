package control_test

import (
	"context"
	"errors"
	"ritual/internal/gui/control"
	"testing"
)

type fakeWindow struct {
	shown   int
	focused int
}

func (f *fakeWindow) Show()  { f.shown++ }
func (f *fakeWindow) Focus() { f.focused++ }

func TestShowLogs_NoFactoryIsNoop(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.ShowLogs() // must not panic without a factory
}

func TestShowLogs_BuildsLazilyAndReuses(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	win := &fakeWindow{}
	built := 0
	svc.SetLogsWindowFactory(func() control.WindowControl {
		built++
		return win
	})

	svc.ShowLogs()
	svc.ShowLogs()

	if built != 1 {
		t.Fatalf("factory built %d times, want 1 (lazy, cached)", built)
	}
	if win.shown != 2 || win.focused != 2 {
		t.Fatalf("show=%d focus=%d, want 2/2", win.shown, win.focused)
	}
}

func TestReadServerLog_NilReaderReturnsNil(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	if got := svc.ReadServerLog(); got != nil {
		t.Fatalf("nil reader should yield nil, got %v", got)
	}
}

func TestReadServerLog_ReturnsLines(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.SetConsoleReader(func(context.Context) ([]string, error) {
		return []string{"boot", "ready"}, nil
	})
	got := svc.ReadServerLog()
	if len(got) != 2 || got[0] != "boot" || got[1] != "ready" {
		t.Fatalf("lines = %v, want [boot ready]", got)
	}
}

func TestReadServerLog_ErrorDegradesToNil(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.SetConsoleReader(func(context.Context) ([]string, error) {
		return nil, errors.New("read boom")
	})
	if got := svc.ReadServerLog(); got != nil {
		t.Fatalf("reader error should yield nil, got %v", got)
	}
}
