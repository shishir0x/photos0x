package core

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultAutoBackupInterval = 15 * time.Minute
)

// AutoBackupManager periodically scans configured folders and triggers automatic
// uploads of newly added or changed media files through UploadManager.
type AutoBackupManager struct {
	uploadManager  *UploadManager
	logger         *slog.Logger
	history        *AutoBackupHistory
	onStatusChange func(bool, []string)

	mu       sync.Mutex
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	scanning bool
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

// Start begins periodic scanning of configured backup folders and immediately runs an initial scan. Idempotent.
func (m *AutoBackupManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	cfg := currentConfig().AutoBackup
	folders := cleanMonitoredFolders(cfg.Folders)
	if len(folders) == 0 {
		m.logger.Info("auto-backup: no folders configured, periodic scan not started")
		return nil
	}

	interval := defaultAutoBackupInterval
	if cfg.IntervalMinutes > 0 {
		interval = time.Duration(cfg.IntervalMinutes) * time.Minute
	}

	m.ticker = time.NewTicker(interval)
	m.stopCh = make(chan struct{})
	m.running = true

	go m.loop()
	go m.scanFolders(folders)

	m.logger.Info("auto-backup: started", "folders", folders, "interval", interval)
	if m.onStatusChange != nil {
		m.onStatusChange(true, folders)
	}
	return nil
}

// Stop shuts down periodic scanning cleanly. Idempotent.
func (m *AutoBackupManager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}

	m.running = false
	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	if m.ticker != nil {
		m.ticker.Stop()
		m.ticker = nil
	}
	m.mu.Unlock()

	m.logger.Info("auto-backup: stopped")
	if m.onStatusChange != nil {
		m.onStatusChange(false, currentConfig().AutoBackup.Folders)
	}
}

// IsRunning reports whether the periodic scanner is currently active.
func (m *AutoBackupManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Restart stops any running scanner and starts a fresh one with the current configuration.
func (m *AutoBackupManager) Restart() error {
	m.Stop()

	cfg := currentConfig().AutoBackup
	if !cfg.Enabled || len(cfg.Folders) == 0 {
		return nil
	}
	return m.Start()
}

// TriggerScan schedules an immediate scan of all configured backup folders.
func (m *AutoBackupManager) TriggerScan() {
	m.mu.Lock()
	folders := cleanMonitoredFolders(currentConfig().AutoBackup.Folders)
	running := m.running
	m.mu.Unlock()

	if running && len(folders) > 0 {
		go m.scanFolders(folders)
	}
}

// loop runs the periodic scanning interval.
func (m *AutoBackupManager) loop() {
	m.mu.Lock()
	ticker := m.ticker
	stopCh := m.stopCh
	m.mu.Unlock()

	if ticker == nil || stopCh == nil {
		return
	}

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			cfg := currentConfig().AutoBackup
			folders := cleanMonitoredFolders(cfg.Folders)
			if len(folders) > 0 {
				m.scanFolders(folders)
			}
		}
	}
}

// scanFolders walks monitored folders, discovers new or modified media files,
// and forwards them directly to UploadManager.
func (m *AutoBackupManager) scanFolders(folders []string) {
	m.mu.Lock()
	if m.scanning || !m.running {
		m.mu.Unlock()
		return
	}
	m.scanning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.scanning = false
		m.mu.Unlock()
	}()

	folders = cleanMonitoredFolders(folders)
	if len(folders) == 0 {
		return
	}

	m.logger.Info("auto-backup: scanning folders", "folders", folders)
	opts := currentConfig().Preferences.UploadOptions()
	var readyPaths []string

	for _, folder := range folders {
		_ = filepath.WalkDir(folder, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			m.mu.Lock()
			running := m.running
			m.mu.Unlock()
			if !running {
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

			// If already uploaded with matching size and modtime, skip it
			if m.history.IsUploaded(path, info.Size(), info.ModTime().Unix()) {
				return nil
			}

			readyPaths = append(readyPaths, path)
			return nil
		})
	}

	if len(readyPaths) == 0 {
		m.logger.Info("auto-backup: scan completed, no new files")
		return
	}

	m.logger.Info("auto-backup: scan completed", "newFiles", len(readyPaths))

	if m.uploadManager == nil {
		m.logger.Warn("auto-backup: upload manager is nil; pending files not uploaded")
		return
	}

	// Sort low-to-high storage before dispatching
	sortPathsBySizeAscending(readyPaths)

	uploadOpts := opts
	uploadOpts.Recursive = true
	uploadOpts.DeleteFromHost = false
	uploadOpts.OnFileUploaded = func(path string, size int64, modTime int64) {
		_ = m.history.MarkUploaded(path, size, modTime)
	}

	m.uploadManager.Upload(readyPaths, uploadOpts)
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
