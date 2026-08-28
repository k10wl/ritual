package control_test

import (
	"context"
	"ritual/internal/gui/control"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func readerReturning(lines []string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return lines, nil }
}

func TestControlService_SetConsoleReaderCalledTwice_LaterCallWins(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.SetConsoleReader(readerReturning([]string{"first"}))
	svc.SetConsoleReader(readerReturning([]string{"second"}))

	assert.Equal(t, []string{"second"}, svc.ReadServerLog(), "the later SetConsoleReader call must win — design-log/055 Phase D allows ChangeWorkRoot to call it a second time mid-run")
}

func TestControlService_SetConsoleReaderConcurrentWithReadServerLog_NoRace(t *testing.T) {
	svc := control.NewControlService(nil, nil, nil, nil, nil, nil)
	svc.SetConsoleReader(readerReturning([]string{"boot"}))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			svc.SetConsoleReader(readerReturning([]string{"relocated"}))
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_ = svc.ReadServerLog()
		}
	}()
	wg.Wait()
}
