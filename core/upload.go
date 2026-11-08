package core

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// FilesDroppedEvent is emitted when files are dropped on any drop zone
type FilesDroppedEvent struct {
	Files    []string `json:"files"`
	DropZone string   `json:"dropZone"`
}

// StartUploadEvent is received from frontend to start upload
type StartUploadEvent struct {
	Files []string `json:"files"`
}

// CancelUploadFileEvent is received from frontend to cancel an individual file upload
type CancelUploadFileEvent struct {
	WorkerID int    `json:"workerId"`
	FilePath string `json:"filePath"`
}

type activeWorkerUpload struct {
	workerID int
	paths    []string
	cancel   context.CancelFunc
}

type UploadManager struct {
	mu              sync.Mutex
	wg              sync.WaitGroup
	cancel          chan struct{}
	canceled        bool
	running         bool
	pauseMu         sync.Mutex
	pauseCond       *sync.Cond
	paused          bool
	reporter        UploadReporter
	logger          *slog.Logger
	// apiOptions is captured from the options of the run in progress.
	apiOptions      ApiOptions
	activeUploads   sync.Map // workerID (int) -> *activeWorkerUpload
	cancelledByUser sync.Map // path (string) -> bool
}

// NewUploadManager creates a manager that reports progress to reporter. A nil
// reporter or logger discards output.
func NewUploadManager(reporter UploadReporter, logger *slog.Logger) *UploadManager {
	if reporter == nil {
		reporter = NopReporter{}
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	m := &UploadManager{
		reporter: reporter,
		logger:   logger,
	}
	m.pauseCond = sync.NewCond(&m.pauseMu)
	return m
}

func (m *UploadManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *UploadManager) Pause() {
	m.pauseMu.Lock()
	m.paused = true
	m.pauseMu.Unlock()
}

func (m *UploadManager) Resume() {
	m.pauseMu.Lock()
	m.paused = false
	if m.pauseCond != nil {
		m.pauseCond.Broadcast()
	}
	m.pauseMu.Unlock()
}

func (m *UploadManager) IsPaused() bool {
	m.pauseMu.Lock()
	defer m.pauseMu.Unlock()
	return m.paused
}

func (m *UploadManager) waitIfPaused(workerID int) {
	m.pauseMu.Lock()
	for m.paused && !m.isCancelled() {
		m.reporter.ThreadStatus(ThreadStatus{
			WorkerID: workerID,
			Status:   "idle",
			Message:  "Paused",
		})
		m.pauseCond.Wait()
	}
	m.pauseMu.Unlock()
}

func (m *UploadManager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil && !m.canceled {
		close(m.cancel)
		m.canceled = true
		// Don't set to nil - readers still need to detect closure via select
	}
	m.pauseMu.Lock()
	m.paused = false
	if m.pauseCond != nil {
		m.pauseCond.Broadcast()
	}
	m.pauseMu.Unlock()
}

// CancelWorker cancels the file currently being uploaded by workerID,
// allowing that worker to immediately proceed to the next file in the queue.
func (m *UploadManager) CancelWorker(workerID int) bool {
	if val, ok := m.activeUploads.Load(workerID); ok {
		if active, ok := val.(*activeWorkerUpload); ok {
			for _, p := range active.paths {
				m.cancelledByUser.Store(p, true)
			}
			active.cancel()
			return true
		}
	}
	return false
}

// CancelFile cancels the upload of a specific file path if it is currently in progress.
func (m *UploadManager) CancelFile(filePath string) bool {
	canonical := canonicalUploadPath(filePath)
	var matchedWorker *activeWorkerUpload
	m.activeUploads.Range(func(key, val any) bool {
		if active, ok := val.(*activeWorkerUpload); ok {
			for _, p := range active.paths {
				if canonicalUploadPath(p) == canonical || p == filePath {
					matchedWorker = active
					return false
				}
			}
		}
		return true
	})
	if matchedWorker != nil {
		for _, p := range matchedWorker.paths {
			m.cancelledByUser.Store(p, true)
		}
		matchedWorker.cancel()
		return true
	}
	return false
}

// CancelWorkerOrFile cancels an individual file upload by workerID or filePath.
func (m *UploadManager) CancelWorkerOrFile(workerID int, filePath string) bool {
	if workerID >= 0 && m.CancelWorker(workerID) {
		return true
	}
	if filePath != "" {
		return m.CancelFile(filePath)
	}
	return false
}

// isCancelled checks if cancellation has been requested
func (m *UploadManager) isCancelled() bool {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel == nil {
		return false
	}
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}

// getCancelChan returns the cancel channel safely
func (m *UploadManager) getCancelChan() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancel
}

type UploadBatchStart struct {
	Total      int   `json:"Total"`
	TotalBytes int64 `json:"TotalBytes"`
}

type FileUploadResult struct {
	MediaKey     string   `json:"MediaKey"`
	IsError      bool     `json:"IsError"`
	IsLivePhoto  bool     `json:"IsLivePhoto"`
	Skipped      bool     `json:"Skipped"`
	SkipCode     string   `json:"SkipCode"`
	SkipReason   string   `json:"SkipReason"`
	Error        error    `json:"-"`
	ErrorMessage string   `json:"ErrorMessage"`
	Path         string   `json:"Path"`
	Paths        []string `json:"Paths"`
}

type ThreadStatus struct {
	WorkerID      int    `json:"WorkerID"`
	Status        string `json:"Status"` // "idle", "hashing", "checking", "uploading", "finalizing", "completed", "skipped", "error"
	FilePath      string `json:"FilePath"`
	FileName      string `json:"FileName"`
	Thumbnail     string `json:"Thumbnail,omitempty"`
	Message       string `json:"Message"`
	BytesUploaded int64  `json:"BytesUploaded"`
	BytesTotal    int64  `json:"BytesTotal"`
	Attempt       int    `json:"Attempt"` // Current attempt number (1-based), 0 if not applicable
}

// Upload starts an upload run in the background. It returns true if the run
// was started, or false if an upload run is already in progress. Progress is
// delivered through the manager's UploadReporter; UploadStop marks the end of the run.
func (m *UploadManager) Upload(paths []string, opts UploadOptions) bool {
	opts = opts.normalized()
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return false
	}
	m.running = true
	m.cancel = make(chan struct{})
	m.canceled = false
	m.pauseMu.Lock()
	m.paused = false
	m.pauseMu.Unlock()
	m.apiOptions = opts.Api
	m.mu.Unlock()

	// Make preflight visible immediately so a long directory or metadata scan can
	// be cancelled from the UI.
	m.reporter.UploadStart(UploadBatchStart{})

	targetPaths, err := filterGooglePhotosFilesWithCancel(paths, opts, m.isCancelled)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			m.finish()
			return true
		}
		m.reporter.FileResult(FileUploadResult{
			IsError:      true,
			Error:        err,
			ErrorMessage: err.Error(),
		})
		m.finish()
		return true
	}
	workItems, preflightWarnings := ClassifyUploadWork(targetPaths, LivePhotoClassificationOptions{
		Enabled:             opts.PairLivePhotos,
		SkipIncomplete:      opts.SkipIncompleteLivePhotos,
		IgnoreAppleMetadata: opts.IgnoreAppleMetadata,
		Cancelled:           m.isCancelled,
	}, nil)
	if m.isCancelled() {
		m.finish()
		return true
	}
	m.reportPreflight(len(workItems), preflightWarnings)

	if len(workItems) == 0 {
		m.finish()
		return true
	}

	if _, err := NewApi(opts.Api); err != nil {
		for _, item := range workItems {
			path := uploadWorkPrimaryPath(item)
			if opts.OnFileFailed != nil {
				for _, p := range uploadWorkPaths(item) {
					opts.OnFileFailed(p, err)
				}
			}
			m.reporter.FileResult(FileUploadResult{
				IsError:      true,
				Error:        err,
				ErrorMessage: err.Error(),
				Path:         path,
			})
		}
		m.finish()
		return true
	}

	// Sort work items from lowest storage (smallest total size) to highest storage (largest total size)
	totalBytes := sortUploadWorkItemsBySizeAscending(workItems)
	m.reporter.TotalBytes(totalBytes)

	// Don't start more threads than files to process
	numWorkers := min(opts.Threads, len(workItems))

	// Create a worker pool for concurrent uploads
	workChan := make(chan UploadWorkItem, len(workItems))
	results := make(chan FileUploadResult, len(workItems))

	// Start workers
	for i := range numWorkers {
		m.wg.Add(1)
		go m.runWorker(i, workChan, results, opts)
	}

	// Send work to workers
	go func() {
	LOOP:
		for _, item := range workItems {
			select {
			case <-m.cancel:
				break LOOP
			case workChan <- item:
			}
		}
		close(workChan)
	}()

	// Handle results, wait for completion, and create album if configured
	go func() {
		sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
		sessionStartTime := time.Now().Unix()
		var sessionItems []UploadSessionItem
		var successCount, failCount, skippedCount int

		// Collect successful uploads with path -> mediaKey mapping for AUTO mode
		successfulUploads := make(map[string]string) // path -> mediaKey

		// Wait for all workers to finish in a separate goroutine, then close results
		go func() {
			m.wg.Wait()
			close(results)
		}()

		// Process all results (this blocks until results channel is closed)
		for result := range results {
			m.reporter.FileResult(result)
			status := "success"
			if result.IsError {
				status = "fail"
				failCount++
				m.logger.Error("upload error", "path", result.Path, "error", result.Error)
			} else if result.Skipped {
				status = "skipped"
				skippedCount++
			} else {
				successCount++
				m.logger.Info("upload success", "path", result.Path)
				if result.MediaKey != "" {
					successfulUploads[result.Path] = result.MediaKey
				}
			}

			var fileSize int64
			if info, err := os.Stat(result.Path); err == nil {
				fileSize = info.Size()
			}

			sessionItems = append(sessionItems, UploadSessionItem{
				SessionID:    sessionID,
				Path:         result.Path,
				FileName:     filepath.Base(result.Path),
				Status:       status,
				ErrorMessage: result.ErrorMessage,
				MediaKey:     result.MediaKey,
				Size:         fileSize,
				CreatedAt:    time.Now().Unix(),
			})
		}

		_ = GetDefaultSessionHistory().RecordSession(UploadSession{
			ID:           sessionID,
			StartedAt:    sessionStartTime,
			FinishedAt:   time.Now().Unix(),
			AlbumName:    opts.AlbumName,
			TotalFiles:   len(workItems),
			SuccessCount: successCount,
			FailCount:    failCount,
			SkippedCount: skippedCount,
			TotalBytes:   totalBytes,
		}, sessionItems)

		m.logger.Info("upload complete", "successful", len(successfulUploads),
			"albumName", opts.AlbumName, "albumAutoMode", opts.AlbumAutoMode)

		if len(successfulUploads) > 0 {
			m.handleAlbumCreation(successfulUploads, opts.AlbumName, opts.AlbumAutoMode)
		}

		m.finish()
	}()
	return true
}

// finish signals the end of a run and releases the manager for the next one.
func (m *UploadManager) finish() {
	m.reporter.UploadStop()
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
}

func (m *UploadManager) reportPreflight(uploadItemCount int, warnings []PreflightWarning) {
	skippedCount := 0
	for _, warning := range warnings {
		if isSkippedPreflightWarning(warning.Code) {
			skippedCount++
		}
	}

	// Start before emitting warnings/results so the frontend resets its previous
	// batch while retaining every event from this preflight.
	m.reporter.UploadStart(UploadBatchStart{
		Total:      uploadItemCount + skippedCount,
		TotalBytes: 0,
	})
	for _, warning := range warnings {
		m.reporter.Warning(warning)
		if !isSkippedPreflightWarning(warning.Code) {
			continue
		}
		primaryPath := ""
		if len(warning.Paths) > 0 {
			primaryPath = warning.Paths[0]
		}
		m.reporter.FileResult(FileUploadResult{
			IsLivePhoto: true,
			Skipped:     true,
			SkipCode:    warning.Code,
			SkipReason:  warning.Message,
			Path:        primaryPath,
			Paths:       warning.Paths,
		})
	}
}

func isSkippedPreflightWarning(code string) bool {
	return code == "incomplete-live-photo-skipped" || code == "ambiguous-filename-stem"
}

// handleAlbumCreation handles album creation based on config (manual name/key or AUTO mode)
func (m *UploadManager) handleAlbumCreation(uploads map[string]string, albumName string, albumAutoMode bool) {
	// Check if cancelled before starting album creation
	if m.isCancelled() {
		m.logger.Info("Upload cancelled, skipping album creation")
		return
	}

	m.logger.Info(fmt.Sprintf("handleAlbumCreation called with %d uploads", len(uploads)))

	// Create API once for all album operations
	api, err := NewApi(m.apiOptions)
	if err != nil {
		m.logger.Error(fmt.Sprintf("failed to create API for album creation: %v", err))
		m.reporter.AlbumError(AlbumError{
			AlbumName: albumName,
			Error:     fmt.Sprintf("failed to initialize API: %v", err),
		})
		return
	}

	albumManager := NewAlbumManager(api, m.reporter, m.logger, m.getCancelChan())

	// Check if AUTO mode is enabled
	if albumAutoMode {
		m.logger.Info("AUTO mode enabled, creating albums from directories")
		m.createAlbumsFromDirectories(albumManager, uploads)
		return
	}

	// Manual mode: use AlbumName if set
	if albumName == "" {
		m.logger.Info("No album name set and AUTO mode disabled, skipping album creation")
		return
	}

	m.logger.Info(fmt.Sprintf("Creating album with name/key: '%s'", albumName))

	mediaKeys := make([]string, 0, len(uploads))
	for _, mediaKey := range uploads {
		mediaKeys = append(mediaKeys, mediaKey)
	}

	m.logger.Info(fmt.Sprintf("Adding %d media keys to album '%s'", len(mediaKeys), albumName))

	albumKeys, err := albumManager.AddToAlbum(mediaKeys, albumName)
	if err != nil {
		m.logger.Error(fmt.Sprintf("failed to create album '%s': %v", albumName, err))
		m.reporter.AlbumError(AlbumError{
			AlbumName: albumName,
			Error:     err.Error(),
		})
		return
	}
	m.logger.Info(fmt.Sprintf("created album '%s' with %d items, album keys: %v", albumName, len(mediaKeys), albumKeys))
}

// createAlbumsFromDirectories creates albums based on parent directory names (AUTO mode)
func (m *UploadManager) createAlbumsFromDirectories(albumManager *AlbumManager, uploads map[string]string) {
	// Group media keys by parent directory
	mediaKeysByDir := make(map[string][]string)

	for filePath, mediaKey := range uploads {
		parentDir := filepath.Dir(filePath)
		mediaKeysByDir[parentDir] = append(mediaKeysByDir[parentDir], mediaKey)
	}

	// Create an album for each directory
	for dirPath, mediaKeys := range mediaKeysByDir {
		albumName := filepath.Base(dirPath)
		if albumName == "" || albumName == "." {
			albumName = "Uploads"
		}

		albumKeys, err := albumManager.AddToAlbum(mediaKeys, albumName)
		if err != nil {
			m.logger.Error(fmt.Sprintf("failed to create album '%s': %v", albumName, err))
			m.reporter.AlbumError(AlbumError{
				AlbumName: albumName,
				Error:     err.Error(),
			})
			continue
		}
		m.logger.Info(fmt.Sprintf("created album '%s' with %d items, album keys: %v", albumName, len(mediaKeys), albumKeys))
	}
}

var supportedVideoFormats = map[string]bool{
	"3gp": true, "3g2": true, "asf": true, "avi": true, "divx": true,
	"m2t": true, "m2ts": true, "m4v": true, "mkv": true, "mmv": true,
	"mod": true, "mov": true, "mp4": true, "mpg": true, "mpeg": true,
	"mts": true, "tod": true, "wmv": true, "ts": true, "webm": true,
}

// isVideoFile checks if a file extension corresponds to a video format
func isVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	return supportedVideoFormats[ext[1:]]
}

// supportedFormats is a map of file extensions supported by Google Photos (O(1) lookup)
var supportedFormats = map[string]bool{
	// Photo formats
	"avif": true, "bmp": true, "gif": true, "heic": true, "heif": true, "ico": true,
	"jpg": true, "jpeg": true, "png": true, "tif": true, "tiff": true, "webp": true,
	"cr2": true, "cr3": true, "nef": true, "arw": true, "orf": true,
	"raf": true, "rw2": true, "pef": true, "sr2": true, "dng": true,
	// Video formats
	"3gp": true, "3g2": true, "asf": true, "avi": true, "divx": true,
	"m2t": true, "m2ts": true, "m4v": true, "mkv": true, "mmv": true,
	"mod": true, "mov": true, "mp4": true, "mpg": true, "mpeg": true,
	"mts": true, "tod": true, "wmv": true, "ts": true, "webm": true,
}

// isSupportedByGooglePhotos checks if a file extension is supported by Google Photos
func isSupportedByGooglePhotos(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	// Remove the dot and check map
	return supportedFormats[ext[1:]]
}

func scanDirectoryForFiles(path string, recursive bool, excludePattern string, cancelled func() bool) ([]string, error) {
	var files []string

	if cancelled != nil && cancelled() {
		return nil, context.Canceled
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if cancelled != nil && cancelled() {
			return nil, context.Canceled
		}
		fullPath := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			if excludePattern != "" && entry.Name() == excludePattern {
				continue
			}
			if recursive {
				subFiles, err := scanDirectoryForFiles(fullPath, recursive, excludePattern, cancelled)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return nil, err
					}
					continue
				}
				files = append(files, subFiles...)
			}
		} else {
			files = append(files, fullPath)
		}
	}

	return files, nil
}

// FilterGooglePhotosFiles expands paths into the files that would be uploaded
// under opts: directories are scanned, unsupported files dropped, duplicates removed.
func FilterGooglePhotosFiles(paths []string, opts UploadOptions) ([]string, error) {
	return filterGooglePhotosFilesWithCancel(paths, opts, nil)
}

func filterGooglePhotosFilesWithCancel(paths []string, opts UploadOptions, cancelled func() bool) ([]string, error) {
	var supportedFiles []string
	type seenUploadFile struct {
		canonicalPath string
		info          os.FileInfo
	}
	seenFiles := make(map[string][]seenUploadFile)
	appendFile := func(path string, info os.FileInfo) {
		if !opts.DisableUnsupportedFilesFilter && !isSupportedByGooglePhotos(path) {
			return
		}
		canonicalPath := canonicalUploadPath(path)
		bucketKey := canonicalPath
		if runtime.GOOS == "windows" {
			bucketKey = strings.ToLower(bucketKey)
		}
		if info == nil {
			info, _ = os.Stat(path)
		}
		for _, seen := range seenFiles[bucketKey] {
			if canonicalPath == seen.canonicalPath ||
				(info != nil && seen.info != nil && os.SameFile(info, seen.info)) {
				return
			}
		}
		seenFiles[bucketKey] = append(seenFiles[bucketKey], seenUploadFile{
			canonicalPath: canonicalPath,
			info:          info,
		})
		supportedFiles = append(supportedFiles, path)
	}

	for _, path := range paths {
		if cancelled != nil && cancelled() {
			return nil, context.Canceled
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("error accessing path %s: %w", path, err)
		}

		if fileInfo.IsDir() {
			files, err := scanDirectoryForFiles(path, opts.Recursive, opts.ExcludePattern, cancelled)
			if err != nil {
				return nil, fmt.Errorf("error scanning directory %s: %w", path, err)
			}

			for _, file := range files {
				if cancelled != nil && cancelled() {
					return nil, context.Canceled
				}
				appendFile(file, nil)
			}
		} else {
			appendFile(path, fileInfo)
		}
	}

	sortPathsBySizeAscending(supportedFiles)
	return supportedFiles, nil
}

func canonicalUploadPath(path string) string {
	canonicalPath, err := filepath.Abs(path)
	if err != nil {
		canonicalPath = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(canonicalPath); err == nil {
		canonicalPath = resolved
	}
	return filepath.Clean(canonicalPath)
}

// UploadFile uploads a single file and returns its media key.
func UploadFile(ctx context.Context, api *Api, filePath string, opts UploadOptions, workerID int, reporter UploadReporter) (string, error) {
	if reporter == nil {
		reporter = NopReporter{}
	}
	return uploadSingleFile(ctx, api, filePath, opts, workerID, reporter)
}

func uploadSingleFile(ctx context.Context, api *Api, filePath string, opts UploadOptions, workerID int, reporter UploadReporter) (string, error) {
	fileName := filepath.Base(filePath)
	mediakey := ""

	// Determine the timestamp to use for the upload.
	// Default to file mtime, but allow filename-based timestamp to take precedence if enabled.
	var uploadTimestamp int64
	if info, err := os.Stat(filePath); err == nil {
		uploadTimestamp = info.ModTime().Unix()
	}
	if opts.SetDateFromFilename {
		if t, ok := parseTimestampFromFilename(filePath); ok {
			uploadTimestamp = t.Unix()
		}
	}

	// Stage 1: Hashing
	thumbnail := GenerateThumbnail(filePath, 48)
	reporter.ThreadStatus(ThreadStatus{
		WorkerID:  workerID,
		Status:    "hashing",
		FilePath:  filePath,
		FileName:  fileName,
		Thumbnail: thumbnail,
		Message:   "Hashing...",
	})

	sha1_hash_bytes, err := CalculateSHA1(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("error calculating hash file: %w", err)
	}

	sha1_hash_b64 := base64.StdEncoding.EncodeToString([]byte(sha1_hash_bytes))

	// Stage 2: Checking if exists in library
	if !opts.ForceUpload {
		reporter.ThreadStatus(ThreadStatus{
			WorkerID:  workerID,
			Status:    "checking",
			FilePath:  filePath,
			FileName:  fileName,
			Thumbnail: thumbnail,
			Message:   "Checking if file exists in library...",
		})

		mediakey, err = api.FindRemoteMediaByHash(sha1_hash_bytes)
		if err != nil {
			// Non-fatal: log via callback and continue with upload
			reporter.ThreadStatus(ThreadStatus{
				WorkerID:  workerID,
				Status:    "checking",
				FilePath:  filePath,
				FileName:  fileName,
				Thumbnail: thumbnail,
				Message:   fmt.Sprintf("Hash check warning: %v, proceeding with upload", err),
			})
		}
		if len(mediakey) > 0 {
			reporter.ThreadStatus(ThreadStatus{
				WorkerID:  workerID,
				Status:    "completed",
				FilePath:  filePath,
				FileName:  fileName,
				Thumbnail: thumbnail,
				Message:   "Already in library",
			})
			if opts.DeleteFromHost {
				if err := os.Remove(filePath); err != nil {
					return mediakey, fmt.Errorf("file exists in library but failed to delete local copy: %w", err)
				}
			}
			return mediakey, nil
		}
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("error getting file info: %w", err)
	}

	// Stage 3: Uploading
	fileSize := fileInfo.Size()
	reporter.ThreadStatus(ThreadStatus{
		WorkerID:      workerID,
		Status:        "uploading",
		FilePath:      filePath,
		FileName:      fileName,
		Thumbnail:     thumbnail,
		Message:       "Uploading...",
		BytesUploaded: 0,
		BytesTotal:    fileSize,
	})

	// Create progress callback for upload
	progressCallback := func(bytesUploaded, bytesTotal int64, attempt int) {
		var message string
		switch {
		case attempt == 1:
			message = "Uploading..."
		case bytesUploaded == 0:
			message = fmt.Sprintf("Retrying (new session, attempt %d)...", attempt)
		default:
			message = fmt.Sprintf("Retrying... (attempt %d)", attempt)
		}
		reporter.ThreadStatus(ThreadStatus{
			WorkerID:      workerID,
			Status:        "uploading",
			FilePath:      filePath,
			FileName:      fileName,
			Thumbnail:     thumbnail,
			Message:       message,
			BytesUploaded: bytesUploaded,
			BytesTotal:    bytesTotal,
			Attempt:       attempt,
		})
	}

	// UploadFileWithProgress acquires a fresh Scotty session token before each
	// attempt, so stale sessions from failed attempts never block retries.
	finalizeToken, err := api.UploadFileWithProgress(ctx, filePath, sha1_hash_b64, fileSize, progressCallback)
	if err != nil {
		var permanentErr *PermanentUploadError
		if errors.As(err, &permanentErr) {
			// Surface a user-readable reason without exposing server internals.
			return "", fmt.Errorf("upload rejected by server (HTTP %d) — file may be unsupported or account may lack permission", permanentErr.StatusCode)
		}
		return "", fmt.Errorf("error uploading file: %w", err)
	}
	commitToken, err := finalizeToken.legacyCommitToken()
	if err != nil {
		return "", fmt.Errorf("error decoding upload finalize token: %w", err)
	}

	// Stage 4: Finalizing
	reporter.ThreadStatus(ThreadStatus{
		WorkerID:  workerID,
		Status:    "finalizing",
		FilePath:  filePath,
		FileName:  fileName,
		Thumbnail: thumbnail,
		Message:   "Committing upload...",
	})

	mediaKey, err := api.CommitUpload(commitToken, fileInfo.Name(), sha1_hash_bytes, uploadTimestamp)
	if err != nil {
		return "", fmt.Errorf("error committing file: %w", err)
	}

	if len(mediaKey) == 0 {
		return "", fmt.Errorf("media key not received")
	}

	if opts.DeleteFromHost {
		if err := os.Remove(filePath); err != nil {
			return mediaKey, fmt.Errorf("uploaded successfully but failed to delete file: %w", err)
		}
	}

	return mediaKey, nil
}

func (m *UploadManager) runWorker(workerID int, workChan <-chan UploadWorkItem, results chan<- FileUploadResult, opts UploadOptions) {
	defer m.wg.Done()
	cancel := m.getCancelChan()

	// Emit idle status initially
	m.reporter.ThreadStatus(ThreadStatus{
		WorkerID: workerID,
		Status:   "idle",
		Message:  "Waiting for files...",
	})

	// Create API client once per worker for connection reuse
	api, err := NewApi(opts.Api)
	if err != nil {
		m.reporter.ThreadStatus(ThreadStatus{
			WorkerID: workerID,
			Status:   "error",
			Message:  fmt.Sprintf("Failed to initialize API: %v", err),
		})
		return
	}

	for item := range workChan {
		m.waitIfPaused(workerID)
		select {
		case <-cancel:
			m.reporter.ThreadStatus(ThreadStatus{
				WorkerID: workerID,
				Status:   "idle",
				Message:  "Cancelled",
			})
			return // Stop if cancellation is requested
		default:
			ctx, cancelUpload := context.WithCancel(context.Background())
			path := uploadWorkPrimaryPath(item)
			paths := uploadWorkPaths(item)

			active := &activeWorkerUpload{
				workerID: workerID,
				paths:    paths,
				cancel:   cancelUpload,
			}
			m.activeUploads.Store(workerID, active)

			go func() {
				select {
				case <-cancel:
					cancelUpload()
				case <-ctx.Done():
					// Upload completed or context cancelled
				}
			}()

			isLivePhoto := item.Kind == UploadWorkLivePhoto
			mediaKey, skipped, err := uploadWorkItem(ctx, api, item, opts, workerID, m.reporter)
			m.activeUploads.Delete(workerID)

			// Check if this specific file was individually cancelled by user
			cancelledByUser := false
			if !m.isCancelled() {
				for _, p := range paths {
					if _, ok := m.cancelledByUser.LoadAndDelete(p); ok {
						cancelledByUser = true
					}
				}
			}

			if cancelledByUser {
				var itemBytes int64
				for _, p := range paths {
					if info, statErr := os.Stat(p); statErr == nil {
						itemBytes += info.Size()
					}
				}
				if itemBytes > 0 {
					m.reporter.TotalBytesDelta(-itemBytes)
				}
				results <- FileUploadResult{
					IsLivePhoto: isLivePhoto,
					Skipped:     true,
					SkipCode:    "cancelled-by-user",
					SkipReason:  "Upload cancelled by user",
					Path:        path,
					Paths:       paths,
				}
				m.reporter.ThreadStatus(ThreadStatus{
					WorkerID: workerID,
					Status:   "skipped",
					FilePath: path,
					FileName: filepath.Base(path),
					Message:  "Upload cancelled by user",
				})
			} else if opts.OnFileUploaded != nil && (err == nil || mediaKey != "" || skipped) {
				for _, p := range paths {
					if info, statErr := os.Stat(p); statErr == nil {
						opts.OnFileUploaded(p, info.Size(), info.ModTime().Unix())
					}
				}
			} else if opts.OnFileFailed != nil && err != nil {
				for _, p := range paths {
					opts.OnFileFailed(p, err)
				}
			} else if err != nil && mediaKey != "" {
				results <- FileUploadResult{IsLivePhoto: isLivePhoto, Path: path, Paths: paths, MediaKey: mediaKey}
				m.reporter.Warning(PreflightWarning{
					Paths:   paths,
					Code:    "local-cleanup-failed",
					Message: err.Error(),
				})
				m.reporter.ThreadStatus(ThreadStatus{
					WorkerID: workerID,
					Status:   "completed",
					FilePath: path,
					FileName: filepath.Base(path),
					Message:  fmt.Sprintf("Uploaded, but local cleanup failed: %v", err),
				})
			} else if err != nil {
				results <- FileUploadResult{IsError: true, IsLivePhoto: isLivePhoto, Error: err, ErrorMessage: err.Error(), Path: path, Paths: paths}
				m.reporter.ThreadStatus(ThreadStatus{
					WorkerID: workerID,
					Status:   "error",
					FilePath: path,
					FileName: filepath.Base(path),
					Message:  fmt.Sprintf("Error: %v", err),
				})
			} else if skipped {
				skipCode := "remote-duplicate"
				skipReason := "Skipped because this file already exists remotely"
				if isLivePhoto {
					skipCode = "remote-live-photo-component-exists"
					skipReason = "Skipped because a Live Photo component already exists remotely"
				}
				results <- FileUploadResult{
					IsLivePhoto: isLivePhoto,
					Skipped:     true,
					SkipCode:    skipCode,
					SkipReason:  skipReason,
					Path:        path,
					Paths:       paths,
				}
			} else {
				results <- FileUploadResult{IsLivePhoto: isLivePhoto, Path: path, Paths: paths, MediaKey: mediaKey}
				m.reporter.ThreadStatus(ThreadStatus{
					WorkerID: workerID,
					Status:   "completed",
					FilePath: path,
					FileName: filepath.Base(path),
					Message:  "Completed",
				})
			}
			cancelUpload()

			// Mark as idle after completing file
			m.reporter.ThreadStatus(ThreadStatus{
				WorkerID: workerID,
				Status:   "idle",
				Message:  "Waiting for next file...",
			})
		}
	}

	// Final idle status when no more work
	m.reporter.ThreadStatus(ThreadStatus{
		WorkerID: workerID,
		Status:   "idle",
		Message:  "Finished",
	})
}

func uploadWorkItem(ctx context.Context, api *Api, item UploadWorkItem, opts UploadOptions, workerID int, reporter UploadReporter) (string, bool, error) {
	switch item.Kind {
	case UploadWorkSingle:
		if item.Single == nil || item.LivePhoto != nil {
			return "", false, fmt.Errorf("invalid single-media work item")
		}
		mediaKey, err := uploadSingleFile(ctx, api, item.Single.Path, opts, workerID, reporter)
		return mediaKey, false, err
	case UploadWorkLivePhoto:
		if item.LivePhoto == nil || item.Single != nil {
			return "", false, fmt.Errorf("invalid Live Photo work item")
		}
		return uploadLivePhotoWithCallback(ctx, api, *item.LivePhoto, LivePhotoUploadOptions{
			Policy:                     buildLivePhotoCommitPolicy(api),
			DeleteFromHost:             opts.DeleteFromHost,
			SetDateFromFilename:        opts.SetDateFromFilename,
			UpdateExistingPhotosToLive: opts.UpdateExistingPhotosToLive,
		}, workerID, reporter)
	default:
		return "", false, fmt.Errorf("unsupported upload work kind %q", item.Kind)
	}
}

func uploadWorkPaths(item UploadWorkItem) []string {
	if item.Kind == UploadWorkLivePhoto && item.LivePhoto != nil {
		return []string{item.LivePhoto.PhotoPath, item.LivePhoto.VideoPath}
	}
	if item.Single != nil {
		return []string{item.Single.Path}
	}
	return nil
}

func uploadWorkPrimaryPath(item UploadWorkItem) string {
	paths := uploadWorkPaths(item)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// sortPathsBySizeAscending sorts a slice of file paths by file size ascending (low storage to high storage).
// Equal sizes are sorted by path alphabetically for stable ordering. Missing files default to size 0.
func sortPathsBySizeAscending(paths []string) {
	if len(paths) <= 1 {
		return
	}
	type pathWithSize struct {
		path string
		size int64
	}
	items := make([]pathWithSize, len(paths))
	for i, p := range paths {
		var sz int64
		if info, err := os.Stat(p); err == nil {
			sz = info.Size()
		}
		items[i] = pathWithSize{path: p, size: sz}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].size != items[j].size {
			return items[i].size < items[j].size
		}
		return items[i].path < items[j].path
	})
	for i, item := range items {
		paths[i] = item.path
	}
}

// sortUploadWorkItemsBySizeAscending sorts work items by total file size ascending (low storage to high storage).
// Equal sizes are sorted by primary path alphabetically for stable ordering.
// It returns the sum of all item sizes.
func sortUploadWorkItemsBySizeAscending(workItems []UploadWorkItem) int64 {
	if len(workItems) == 0 {
		return 0
	}
	type workItemWithSize struct {
		item        UploadWorkItem
		size        int64
		primaryPath string
	}
	items := make([]workItemWithSize, len(workItems))
	var totalBytes int64
	for i, item := range workItems {
		var itemSize int64
		for _, p := range uploadWorkPaths(item) {
			if info, err := os.Stat(p); err == nil {
				itemSize += info.Size()
			}
		}
		items[i] = workItemWithSize{
			item:        item,
			size:        itemSize,
			primaryPath: uploadWorkPrimaryPath(item),
		}
		totalBytes += itemSize
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].size != items[j].size {
			return items[i].size < items[j].size
		}
		return items[i].primaryPath < items[j].primaryPath
	})

	for i, s := range items {
		workItems[i] = s.item
	}
	return totalBytes
}
