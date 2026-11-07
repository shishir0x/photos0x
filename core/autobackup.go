package core

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	autoBackupDebounce         = 5 * time.Second
	autoBackupNotReadyDebounce = 20 * time.Second
	maxWatchedDirs             = 500
	maxConsecutiveFailures     = 10
)

type backupFailureInfo struct {
	consecutiveFailures int
	nextRetry           time.Time
	lastErr             string
	lastSize            int64
	lastModTime         int64
}

// AutoBackupManager watches configured folders and triggers automatic uploads
// of newly added or changed media files.
type AutoBackupManager struct {
	uploadManager  *UploadManager
	logger         *slog.Logger
	history        *AutoBackupHistory
	onStatusChange func(bool, []string)

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	ticker   *time.Ticker
	stopCh   chan struct{}
	stopped  bool
	pending  map[string]struct{}
	debounce *time.Timer
	failures map[string]*backupFailureInfo
}

// NewAutoBackupManager creates a manager with persistent upload tracking.
func NewAutoBackupManager(uploadManager *UploadManager, logger *slog.Logger, onStatusChange func(bool, []string)) *AutoBackupManager {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &AutoBackupManager{
		uploadManager:  uploadManager,
		logger:         logger,
		history:        NewAutoBackupHistory(""),
		pending:        make(map[string]struct{}),
		failures:       make(map[string]*backupFailureInfo),
		onStatusChange: onStatusChange,
	}
}

func cleanMonitoredFolders(folders []string) []string {
	var cleaned []string
	for _, f := range folders {
		abs, err := filepath.Abs(filepath.Clean(f))
		if err != nil {
			abs = filepath.Clean(f)
		}
		isSub := false
		for _, existing := range cleaned {
			if areSameFolderPath(existing, abs) || isSubFolderPath(existing, abs) {
				isSub = true
				break
			}
		}
		if !isSub {
			cleaned = append(cleaned, abs)
		}
	}
	return cleaned
}

// Start begins watching the currently configured backup folders and triggers
// an initial scan of all old and existing files. Idempotent.
func (m *AutoBackupManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.watcher != nil {
		return nil // already running
	}

	cfg := currentConfig().AutoBackup
	folders := cleanMonitoredFolders(cfg.Folders)
	if len(folders) == 0 {
		m.logger.Info("auto-backup: no folders configured, watcher not started")
		return nil
	}

	m.history.Load()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	added := 0
	for _, folder := range folders {
		if err := m.addWatchRecursive(w, folder); err != nil {
			m.logger.Warn("auto-backup: cannot watch folder", "path", folder, "err", err)
			continue
		}
		added++
	}

	if added == 0 {
		_ = w.Close()
		m.logger.Warn("auto-backup: no folders could be watched")
		return nil
	}

	m.watcher = w
	m.stopCh = make(chan struct{})
	m.stopped = false

	intervalMinutes := cfg.IntervalMinutes
	if intervalMinutes < 1 {
		intervalMinutes = 15
	}
	m.ticker = time.NewTicker(time.Duration(intervalMinutes) * time.Minute)

	go m.loop()

	// Initial scan: check old/existing files in all watched folders
	go m.scanFolders(folders)

	m.logger.Info("auto-backup: started", "folders", folders, "intervalMinutes", intervalMinutes)
	if m.onStatusChange != nil {
		m.onStatusChange(true, folders)
	}
	return nil
}

// Stop shuts down the watcher and persists the upload history. Idempotent.
func (m *AutoBackupManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.watcher == nil {
		return
	}

	m.stopped = true
	close(m.stopCh)
	_ = m.watcher.Close()
	m.watcher = nil

	if m.ticker != nil {
		m.ticker.Stop()
		m.ticker = nil
	}

	if m.debounce != nil {
		m.debounce.Stop()
		m.debounce = nil
	}
	m.pending = make(map[string]struct{})

	_ = m.history.Save()

	m.logger.Info("auto-backup: stopped")
	if m.onStatusChange != nil {
		m.onStatusChange(false, currentConfig().AutoBackup.Folders)
	}
}

// IsRunning reports whether the watcher is currently active.
func (m *AutoBackupManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.watcher != nil
}

// Restart stops any running watcher and starts a fresh one with the current
// folder list and interval. Used after adding/removing folders or toggling the feature.
func (m *AutoBackupManager) Restart() error {
	m.Stop()

	cfg := currentConfig().AutoBackup
	if !cfg.Enabled || len(cfg.Folders) == 0 {
		return nil
	}
	return m.Start()
}

// TriggerScan manually schedules an immediate scan of all configured backup folders
// and resets backoff counters to allow immediate re-evaluation.
func (m *AutoBackupManager) TriggerScan() {
	m.mu.Lock()
	m.failures = make(map[string]*backupFailureInfo)
	folders := cleanMonitoredFolders(currentConfig().AutoBackup.Folders)
	stopped := m.stopped || m.watcher == nil
	m.mu.Unlock()

	if !stopped && len(folders) > 0 {
		go m.scanFolders(folders)
	}
}

func (m *AutoBackupManager) recordFailure(path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	abs, aErr := filepath.Abs(path)
	if aErr != nil {
		abs = path
	}

	info, _ := os.Stat(abs)
	var size, mtime int64
	if info != nil {
		size = info.Size()
		mtime = info.ModTime().Unix()
	}

	rec, exists := m.failures[abs]
	if !exists {
		rec = &backupFailureInfo{
			lastSize:    size,
			lastModTime: mtime,
		}
		m.failures[abs] = rec
	}
	rec.consecutiveFailures++
	rec.lastErr = err.Error()

	// Bounded exponential backoff: 2m, 8m, 32m, 2h, max 4h
	shift := min(rec.consecutiveFailures-1, 6)
	backoff := time.Duration(1<<shift) * 2 * time.Minute
	if backoff > 4*time.Hour {
		backoff = 4 * time.Hour
	}
	rec.nextRetry = time.Now().Add(backoff)
	m.logger.Warn("auto-backup: file upload failed; backing off",
		"path", abs,
		"attempts", rec.consecutiveFailures,
		"retryAfter", rec.nextRetry,
		"err", err)
}

func (m *AutoBackupManager) clearFailure(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	delete(m.failures, abs)
}

func (m *AutoBackupManager) shouldSkipFailure(path string, size int64, modTime int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	rec, exists := m.failures[abs]
	if !exists {
		return false
	}

	// If file was modified or resized since the failure, reset failure and allow retry
	if rec.lastSize != size || rec.lastModTime != modTime {
		delete(m.failures, abs)
		return false
	}

	// If maximum consecutive failures reached, quarantine file until modified or manually rescanned
	if rec.consecutiveFailures >= maxConsecutiveFailures {
		return true
	}

	// If backoff window has not elapsed, skip it
	return time.Now().Before(rec.nextRetry)
}

// addWatchRecursive walks a folder tree and registers directories with the watcher
// up to maxWatchedDirs to prevent exhausting operating-system watcher handles.
func (m *AutoBackupManager) addWatchRecursive(w *fsnotify.Watcher, root string) error {
	fi, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", root)
	}
	if w == nil {
		return nil
	}

	excludePattern := currentConfig().Preferences.UploadOptions().ExcludePattern
	count := 0
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if excludePattern != "" && d.Name() == excludePattern {
				return filepath.SkipDir
			}
			if count >= maxWatchedDirs {
				m.logger.Warn("auto-backup: reached maximum watched directories limit; subdirectories will be backed up via periodic scan",
					"root", root, "limit", maxWatchedDirs)
				return filepath.SkipDir
			}
			if err := w.Add(path); err != nil {
				m.logger.Warn("auto-backup: cannot watch subfolder", "path", path, "err", err)
			} else {
				count++
			}
		}
		return nil
	})
}

// scanFolders recursively scans the given folders for existing files that haven't
// been uploaded yet and schedules them for backup.
func (m *AutoBackupManager) scanFolders(folders []string) {
	folders = cleanMonitoredFolders(folders)
	m.logger.Info("auto-backup: scanning folders", "folders", folders)
	queued := 0

	opts := currentConfig().Preferences.UploadOptions()

	for _, folder := range folders {
		_ = filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			m.mu.Lock()
			stopped := m.stopped
			m.mu.Unlock()
			if stopped {
				return filepath.SkipAll
			}

			if d.IsDir() {
				if opts.ExcludePattern != "" && d.Name() == opts.ExcludePattern {
					return filepath.SkipDir
				}
				return nil
			}

			if isTemporaryDownloadFile(path) {
				return nil
			}

			info, err := d.Info()
			if err != nil || info.Size() == 0 {
				return nil
			}

			if !opts.DisableUnsupportedFilesFilter && !isSupportedByGooglePhotos(path) {
				return nil
			}

			// If already uploaded and unmodified, skip it
			if m.history.IsUploaded(path, info.Size(), info.ModTime().Unix()) {
				return nil
			}

			// If file previously failed and is still within backoff window, skip it
			if m.shouldSkipFailure(path, info.Size(), info.ModTime().Unix()) {
				return nil
			}

			m.schedule(path)
			queued++
			return nil
		})
	}

	m.logger.Info("auto-backup: scan completed", "newOrModifiedFiles", queued)
}

// loop is the main event-dispatch goroutine.
func (m *AutoBackupManager) loop() {
	m.mu.Lock()
	w := m.watcher
	ticker := m.ticker
	stopCh := m.stopCh
	m.mu.Unlock()

	if w == nil {
		return
	}

	for {
		select {
		case <-stopCh:
			return

		case <-ticker.C:
			// Periodic recurring scan
			cfg := currentConfig().AutoBackup
			folders := cleanMonitoredFolders(cfg.Folders)
			if len(folders) > 0 {
				// Try re-adding any folders that were previously disconnected/unavailable
				for _, folder := range folders {
					_ = m.addWatchRecursive(w, folder)
				}
				go m.scanFolders(folders)
			}

		case event, ok := <-w.Events:
			if !ok {
				return
			}

			// If a new directory was created or renamed into the tree, watch it and scan it
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
					_ = m.addWatchRecursive(w, event.Name)
					go m.scanFolders([]string{event.Name})
					continue
				}
			}

			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
				m.schedule(event.Name)
			}

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			m.logger.Warn("auto-backup: watcher error", "err", err)
		}
	}
}

// schedule adds a path to the pending set and arms (or resets) the debounce timer.
func (m *AutoBackupManager) schedule(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	m.pending[abs] = struct{}{}

	if m.debounce != nil {
		m.debounce.Reset(autoBackupDebounce)
		return
	}
	m.debounce = time.AfterFunc(autoBackupDebounce, m.flush)
}

// isTemporaryDownloadFile detects transient download/temp files from browsers or apps.
func isTemporaryDownloadFile(filename string) bool {
	base := filepath.Base(filename)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "~$") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".crdownload", ".part", ".download", ".tmp", ".temp", ".aria2", ".partial":
		return true
	}
	return false
}

// isFileReadyForUpload verifies that a file is not in the middle of being copied,
// written, or downloaded by another process.
func isFileReadyForUpload(path string, info os.FileInfo) bool {
	if isTemporaryDownloadFile(path) {
		return false
	}
	if info == nil {
		var err error
		info, err = os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}

	// If file was modified within the last second, give it time to settle
	if time.Since(info.ModTime()) < 1*time.Second {
		return false
	}

	// Attempt to open read-write to verify no exclusive write lock is held by copier/downloader
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		_ = f.Close()
		return true
	}

	// On Windows, error 32 is ERROR_SHARING_VIOLATION and 33 is ERROR_LOCK_VIOLATION
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == 32 || errno == 33) {
		return false // Actively locked by writer/copier
	}

	// If file is marked read-only on disk, O_RDWR will return os.ErrPermission;
	// test read-only open to confirm readability.
	if errors.Is(err, os.ErrPermission) {
		ro, roErr := os.Open(path)
		if roErr == nil {
			_ = ro.Close()
			return true
		}
	}

	return false
}

// flush drains the pending set and starts an upload run.
func (m *AutoBackupManager) flush() {
	m.mu.Lock()

	if m.stopped {
		m.mu.Unlock()
		return
	}

	// If no upload manager is configured, do not panic or discard pending files;
	// keep them in queue until a manager is attached.
	if m.uploadManager == nil {
		m.logger.Warn("auto-backup: upload manager is nil; pending files retained")
		m.mu.Unlock()
		return
	}

	// If another upload run is currently active, don't drop pending files;
	// retry after a short delay once the current batch completes.
	if m.uploadManager.IsRunning() {
		if m.debounce != nil {
			m.debounce.Stop()
		}
		m.debounce = time.AfterFunc(3*time.Second, m.flush)
		m.mu.Unlock()
		return
	}

	pendingCopy := make([]string, 0, len(m.pending))
	for p := range m.pending {
		pendingCopy = append(pendingCopy, p)
	}
	m.pending = make(map[string]struct{})
	m.debounce = nil

	m.mu.Unlock()

	if len(pendingCopy) == 0 {
		return
	}

	opts := currentConfig().Preferences.UploadOptions()
	opts.Recursive = true
	// CRITICAL FIX: Auto Backup must never delete local files from monitored directories!
	opts.DeleteFromHost = false

	opts.OnFileUploaded = func(path string, size int64, modTime int64) {
		_ = m.history.MarkUploaded(path, size, modTime)
		m.clearFailure(path)
	}
	opts.OnFileFailed = func(path string, err error) {
		m.recordFailure(path, err)
	}

	var readyPaths []string
	var retryPaths []string

	for _, p := range pendingCopy {
		if isTemporaryDownloadFile(p) {
			continue
		}

		info, err := os.Stat(p)
		if err != nil {
			// File disappeared
			continue
		}

		if info.IsDir() {
			continue
		}

		if info.Size() == 0 {
			continue
		}

		if !opts.DisableUnsupportedFilesFilter && !isSupportedByGooglePhotos(p) {
			continue
		}

		// Skip if already recorded in history with identical size and modtime
		if m.history.IsUploaded(p, info.Size(), info.ModTime().Unix()) {
			continue
		}

		// Skip if file previously failed and is still within backoff window
		if m.shouldSkipFailure(p, info.Size(), info.ModTime().Unix()) {
			continue
		}

		// Check if file is ready and not locked by an active writer/copier
		if !isFileReadyForUpload(p, info) {
			retryPaths = append(retryPaths, p)
			continue
		}

		readyPaths = append(readyPaths, p)
	}

	// Re-queue any files that were not ready yet
	if len(retryPaths) > 0 {
		m.mu.Lock()
		for _, rp := range retryPaths {
			m.pending[rp] = struct{}{}
		}
		if m.debounce == nil && !m.stopped {
			m.debounce = time.AfterFunc(autoBackupNotReadyDebounce, m.flush)
		}
		m.mu.Unlock()
	}

	if len(readyPaths) == 0 {
		return
	}

	m.logger.Info("auto-backup: triggering upload", "files", len(readyPaths))
	if !m.uploadManager.Upload(readyPaths, opts) {
		// Upload manager was busy, re-queue readyPaths and retry after delay
		m.logger.Info("auto-backup: upload manager busy, re-queueing ready files", "files", len(readyPaths))
		m.mu.Lock()
		for _, rp := range readyPaths {
			m.pending[rp] = struct{}{}
		}
		if m.debounce == nil && !m.stopped {
			m.debounce = time.AfterFunc(3*time.Second, m.flush)
		}
		m.mu.Unlock()
		return
	}
}
