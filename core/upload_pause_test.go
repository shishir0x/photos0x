package core

import (
	"sync"
	"testing"
	"time"
)

func TestUploadManagerPauseResume(t *testing.T) {
	mgr := NewUploadManager(nil, nil)

	if mgr.IsPaused() {
		t.Fatal("expected IsPaused to be false initially")
	}

	mgr.Pause()
	if !mgr.IsPaused() {
		t.Fatal("expected IsPaused to be true after Pause()")
	}

	// Verify waitIfPaused blocks while paused
	var waitCompleted bool
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		mgr.waitIfPaused(0)
		mu.Lock()
		waitCompleted = true
		mu.Unlock()
		close(done)
	}()

	// Give goroutine a moment to enter waitIfPaused
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if waitCompleted {
		t.Fatal("waitIfPaused should block while paused")
	}
	mu.Unlock()

	// Resume should unblock
	mgr.Resume()
	if mgr.IsPaused() {
		t.Fatal("expected IsPaused to be false after Resume()")
	}

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waitIfPaused did not unblock after Resume()")
	}
}

