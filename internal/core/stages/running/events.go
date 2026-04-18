package running

import "fmt"

// ServerStartingInfo fires when the server process has been launched.
type ServerStartingInfo struct{}

func (ServerStartingInfo) String() string { return "server starting" }

// ServerReadyInfo fires once the TCP readiness probe succeeds.
type ServerReadyInfo struct{}

func (ServerReadyInfo) String() string { return "server ready" }

// ServerOutputInfo carries a single line of server stdout/stderr.
type ServerOutputInfo struct{ Line string }

func (s ServerOutputInfo) String() string { return s.Line }

// ServerStopRequestedInfo fires when a stop has been requested.
type ServerStopRequestedInfo struct{}

func (ServerStopRequestedInfo) String() string { return "server stop requested" }

// ServerStoppingInfo fires when the "stop" command has been sent.
type ServerStoppingInfo struct{}

func (ServerStoppingInfo) String() string { return "server stopping" }

// ServerStoppedInfo fires on any graceful terminal state. Forced=true means
// the server did not exit within the grace period and was TerminateProcess'd.
type ServerStoppedInfo struct{ Forced bool }

func (s ServerStoppedInfo) String() string {
	if s.Forced {
		return "server stopped (forced)"
	}
	return "server stopped"
}

// ServerCrashedInfo fires when cmd.Wait returns a non-ctx error.
type ServerCrashedInfo struct{ Err error }

func (s ServerCrashedInfo) String() string { return fmt.Sprintf("server crashed: %v", s.Err) }

// SaveRequested asks the server to flush world state to disk.
type SaveRequested struct{}

func (SaveRequested) String() string { return "save requested" }

// SaveCompleted fires when the flush operation acknowledges completion.
type SaveCompleted struct{}

func (SaveCompleted) String() string { return "save completed" }
