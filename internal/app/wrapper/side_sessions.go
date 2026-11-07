package wrapper

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

// Each side child owns its translator, response gates, writers and lifetime.
// Nothing here changes the primary child's thread or execution ownership.
type claudeSideSessions struct {
	mu       sync.Mutex
	sessions map[string]*claudeSideSession
}

type claudeSideSession struct {
	ctx       context.Context
	cancel    context.CancelFunc
	threadID  string
	runtime   *claudeBackendRuntime
	writeCh   chan []byte
	responses *commandResponseTracker
	done      chan struct{}
	ready     bool // guarded by manager.mu
}

func newClaudeSideSessions() *claudeSideSessions {
	return &claudeSideSessions{sessions: make(map[string]*claudeSideSession)}
}

func (m *claudeSideSessions) closeAll() {
	m.mu.Lock()
	var done []chan struct{}
	for id, session := range m.sessions {
		session.cancel()
		done = append(done, session.done)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	timer := time.NewTimer(wrapperChildWaitTimeout)
	defer timer.Stop()
	for _, ch := range done {
		select {
		case <-ch:
		case <-timer.C:
			return
		}
	}
}

func (m *claudeSideSessions) handle(ctx context.Context, app *App, command agentproto.Command, client *relayws.Client, logger *debuglog.RawLogger) error {
	id := strings.TrimSpace(command.SideSessionID)
	if id == "" {
		return fmt.Errorf("side session ID is required")
	}
	emit := func(status, threadID, message string) {
		_ = client.SendEvents([]agentproto.Event{{Kind: agentproto.EventSideSessionUpdated, SideSessionID: id, CommandID: command.CommandID, ThreadID: threadID, Status: status, ErrorMessage: message}})
	}
	m.mu.Lock()
	session := m.sessions[id]
	if command.Kind == agentproto.CommandSideSessionClose {
		delete(m.sessions, id)
		m.mu.Unlock()
		if session != nil {
			session.cancel()
		}
		emit("closed", "", "")
		return nil
	}
	if command.Kind != agentproto.CommandSideSessionOpen {
		m.mu.Unlock()
		if session == nil {
			return fmt.Errorf("side session is closed")
		}
		command.Target.ThreadID = session.threadID
		if command.Kind == agentproto.CommandRequestRespond {
			prefix := "side/" + id + "/"
			if !strings.HasPrefix(command.Request.RequestID, prefix) {
				return fmt.Errorf("approval belongs to another session")
			}
			command.Request.RequestID = strings.TrimPrefix(command.Request.RequestID, prefix)
		}
		command.Target.ExecutionMode = agentproto.PromptExecutionModeResumeExisting
		result, err := session.runtime.TranslateCommand(command)
		if err != nil {
			return err
		}
		if result.Restart != nil {
			return fmt.Errorf("side session cannot restart the primary child")
		}
		return executeCommandPhases(session.ctx, session.writeCh, session.responses, command.CommandID, result.Phases, app.debugf)
	}
	if session != nil {
		ready := session.ready
		m.mu.Unlock()
		if ready {
			emit("open", session.threadID, "")
		}
		return nil
	}
	if strings.TrimSpace(command.Target.ThreadID) == "" {
		m.mu.Unlock()
		return fmt.Errorf("side session requires a parent thread")
	}
	main := app.runtime.(*claudeBackendRuntime)
	main.mu.Lock()
	resume, found, err := main.lookupStoredResumeTarget(agentproto.PromptDispatchPlan{ExecutionMode: agentproto.PromptExecutionModeResumeExisting, ExecutionThreadID: command.Target.ThreadID, CWD: command.Target.CWD})
	snapshot := main.translator.RuntimeStateSnapshot()
	main.mu.Unlock()
	if err != nil || !found || resume == nil {
		m.mu.Unlock()
		if err != nil {
			return err
		}
		return fmt.Errorf("parent Claude session is unavailable")
	}
	threadID := uuid.NewString()
	resume.ForkEphemeral = true
	resume.SideThreadID = threadID
	if snapshot.SessionID == command.Target.ThreadID {
		resume.Model = snapshot.Model
		resume.PermissionMode = snapshot.NativePermissionMode
	}
	childCtx, cancel := context.WithCancel(ctx)
	runtime := &claudeBackendRuntime{translator: claude.NewTranslator(app.config.InstanceID + "/side/" + id), workspaceRoot: app.config.WorkspaceRoot}
	runtime.translator.PrepareForChildLaunch(threadID)
	session = &claudeSideSession{ctx: childCtx, cancel: cancel, threadID: threadID, runtime: runtime, writeCh: make(chan []byte, 128), responses: newCommandResponseTracker(), done: make(chan struct{})}
	m.sessions[id] = session
	m.mu.Unlock()
	// Launch asynchronously so quit can cancel an initialize handshake in flight.
	go func() {
		defer close(session.done)
		child, err := app.launchClaudeChildSession(childCtx, logger, nil, resume)
		if err != nil {
			m.mu.Lock()
			current := m.sessions[id] == session
			if current {
				delete(m.sessions, id)
			}
			m.mu.Unlock()
			if current {
				emit("failed", "", err.Error())
			}
			cancel()
			return
		}
		defer stopChildSession(child, app.debugf)
		m.mu.Lock()
		current := m.sessions[id] == session && childCtx.Err() == nil
		if current {
			session.ready = true
			emit("open", threadID, "")
		}
		m.mu.Unlock()
		if !current {
			return
		}
		errors := make(chan error, 8)
		tagged := &sideSessionRuntime{backendRuntime: runtime, id: id}
		startChildSessionIO(childCtx, child, io.Discard, io.Discard, session.writeCh, tagged, client, session.responses, newRuntimeTurnTracker(), nil, 1, errors, app.debugf, logger, nil)
		select {
		case <-childCtx.Done():
		case <-child.stdoutDone:
		case <-errors:
		}
		cancel()
		m.mu.Lock()
		current = m.sessions[id] == session
		if current {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
		if current {
			emit("closed", threadID, "Side session ended.")
		}
	}()
	return nil
}

type sideSessionRuntime struct {
	backendRuntime
	id string
}

func (r *sideSessionRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	result, err := r.backendRuntime.ObserveServer(line)
	for i := range result.Events {
		result.Events[i].SideSessionID = r.id
		if result.Events[i].RequestID != "" {
			result.Events[i].RequestID = "side/" + r.id + "/" + result.Events[i].RequestID
		}
	}
	return result, err
}
