package wrapper

import (
	"bytes"
	"time"
)

const wrapperSideDisconnectTimeout = 2 * time.Second

// Cleanup shares the existing child writer, preserving its serialization with
// native main traffic. A single packet avoids partially queuing many sessions;
// the deadline keeps a disconnected/full writer from blocking relay recovery.
func disconnectRuntimeSideSessions(runtime backendRuntime, writeCh chan<- []byte, timeout time.Duration, debugf func(string, ...any)) bool {
	cleanup, ok := runtime.(interface{ DisconnectSideSessions() [][]byte })
	if !ok {
		return true
	}
	frames := cleanup.DisconnectSideSessions()
	if len(frames) == 0 {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case writeCh <- bytes.Join(frames, nil):
		return true
	case <-timer.C:
		if debugf != nil {
			debugf("side session disconnect cleanup could not reach child writer before deadline: frames=%d", len(frames))
		}
		return false
	}
}
