package daemon

import (
	"testing"
	"time"
)

// TestNoteSurfaceResumeFailedAttemptCountsPerTick verifies the give-up counter
// counts at most once per tick (so the synchronous recovery loop and the async
// outcome scan sharing the same `now` cannot double-count) and re-arms on a
// changed failure code.
func TestNoteSurfaceResumeFailedAttemptCountsPerTick(t *testing.T) {
	t.Parallel()

	rec := &surfaceResumeRecoveryState{}
	t1 := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(surfaceResumeRetryBackoff)
	t3 := t2.Add(surfaceResumeRetryBackoff)

	noteSurfaceResumeFailedAttemptLocked(rec, "thread_busy", t1)
	if rec.FailureCount != 1 {
		t.Fatalf("after first failure want 1, got %d", rec.FailureCount)
	}
	// The chokepoint sets these after counting; mirror that for the next calls.
	rec.LastAttemptAt = t1
	rec.LastFailureCode = "thread_busy"

	// Same tick (same now) must not double-count.
	noteSurfaceResumeFailedAttemptLocked(rec, "thread_busy", t1)
	if rec.FailureCount != 1 {
		t.Fatalf("same-tick re-observation must not double count, got %d", rec.FailureCount)
	}

	// Next tick, same code -> increment.
	noteSurfaceResumeFailedAttemptLocked(rec, "thread_busy", t2)
	if rec.FailureCount != 2 {
		t.Fatalf("next-tick same-code want 2, got %d", rec.FailureCount)
	}
	rec.LastAttemptAt = t2
	rec.LastFailureCode = "thread_busy"

	// A different failure code re-arms the budget.
	noteSurfaceResumeFailedAttemptLocked(rec, "thread_not_found", t3)
	if rec.FailureCount != 1 {
		t.Fatalf("code change must reset to 1, got %d", rec.FailureCount)
	}
}

// TestSurfaceResumeGiveUpEmitsOnceThenStaysQuiet verifies the surface hands off
// exactly once when the retry budget is exhausted, then stays quiet.
func TestSurfaceResumeGiveUpEmitsOnceThenStaysQuiet(t *testing.T) {
	t.Parallel()

	app := &App{}
	rec := &surfaceResumeRecoveryState{FailureCount: surfaceResumeMaxFailedAttempts - 1}

	if ev, gaveUp := app.surfaceResumeGiveUpLocked(rec, "surface-1", false); gaveUp || ev != nil {
		t.Fatalf("below budget must not give up: ev=%v gaveUp=%v", ev, gaveUp)
	}

	rec.FailureCount = surfaceResumeMaxFailedAttempts
	ev, gaveUp := app.surfaceResumeGiveUpLocked(rec, "surface-1", false)
	if !gaveUp {
		t.Fatal("at budget must give up")
	}
	if ev == nil || ev.Notice == nil || ev.Notice.Code != "surface_resume_give_up" {
		t.Fatalf("expected one-time headless give-up notice, got %+v", ev)
	}
	if !rec.GaveUp {
		t.Fatal("GaveUp must be set after the hand-off notice")
	}

	// Subsequent ticks: still skip, but no repeated notice.
	if ev2, gaveUp2 := app.surfaceResumeGiveUpLocked(rec, "surface-1", false); !gaveUp2 || ev2 != nil {
		t.Fatalf("after give-up must stay quiet: ev=%v gaveUp=%v", ev2, gaveUp2)
	}
}

// TestSurfaceResumeGiveUpVSCodeNotice verifies the VS Code surface gets its own
// give-up notice variant.
func TestSurfaceResumeGiveUpVSCodeNotice(t *testing.T) {
	t.Parallel()

	app := &App{}
	rec := &surfaceResumeRecoveryState{FailureCount: surfaceResumeMaxFailedAttempts}
	ev, gaveUp := app.surfaceResumeGiveUpLocked(rec, "surface-2", true)
	if !gaveUp || ev == nil || ev.Notice == nil || ev.Notice.Code != "vscode_resume_give_up" {
		t.Fatalf("expected vscode give-up notice, got ev=%+v gaveUp=%v", ev, gaveUp)
	}
}
