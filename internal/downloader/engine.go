package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yebai/better-download-manager/internal/proxy"
)

// DefaultConnections is the number of parallel connections used per task when
// the caller does not specify one (matches IDM's common default).
const DefaultConnections = 8

// Config configures an Engine. The callbacks let the host (Wails service) react
// to task updates without the engine depending on the UI or storage layers.
type Config struct {
	MaxConcurrent int                               // max simultaneously downloading tasks
	ClientFactory func(proxy.Settings) *http.Client // builds the HTTP client (proxy-aware)
	OnUpdate      func(info TaskInfo)               // throttled progress + status changes
	OnPersist     func(rec Record)                  // durable state changes (status only)
	OnRemoved     func(id string)                   // task removed
}

// Engine manages the task queue, scheduling and lifecycle of downloads.
type Engine struct {
	cfg Config

	mu          sync.Mutex
	tasks       map[string]*managed
	order       []string
	activeCount int
	closed      bool
}

type managed struct {
	task    *Task
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
	removed bool
}

// NewEngine creates an engine with sensible defaults applied to cfg.
func NewEngine(cfg Config) *Engine {
	if cfg.MaxConcurrent < 1 {
		cfg.MaxConcurrent = 5
	}
	if cfg.ClientFactory == nil {
		cfg.ClientFactory = func(proxy.Settings) *http.Client { return &http.Client{} }
	}
	if cfg.OnUpdate == nil {
		cfg.OnUpdate = func(TaskInfo) {}
	}
	if cfg.OnPersist == nil {
		cfg.OnPersist = func(Record) {}
	}
	if cfg.OnRemoved == nil {
		cfg.OnRemoved = func(string) {}
	}
	return &Engine{cfg: cfg, tasks: map[string]*managed{}}
}

// ErrNotFound is returned when an operation references an unknown task id.
var ErrNotFound = errors.New("task not found")

// AddOptions describes a new download to add.
type AddOptions struct {
	ID          string
	URL         string
	Filename    string
	SavePath    string
	Category    string
	Connections int
	Headers     map[string]string
	Proxy       proxy.Settings
	AutoStart   bool
}

// Add registers a new task. When AutoStart is true it is queued immediately.
func (e *Engine) Add(opts AddOptions) (TaskInfo, error) {
	conns := opts.Connections
	if conns < 1 {
		conns = DefaultConnections
	}
	t := &Task{
		ID:          opts.ID,
		URL:         opts.URL,
		Filename:    opts.Filename,
		SavePath:    opts.SavePath,
		Category:    opts.Category,
		TotalSize:   -1,
		Connections: conns,
		Headers:     opts.Headers,
		Proxy:       opts.Proxy,
		Status:      StatusQueued,
		CreatedAt:   time.Now(),
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return TaskInfo{}, errors.New("engine closed")
	}
	e.tasks[t.ID] = &managed{task: t}
	e.order = append(e.order, t.ID)
	e.mu.Unlock()

	if !opts.AutoStart {
		t.setStatus(StatusPaused, "")
	}
	// Emit an update so any window (incl. the main list) shows the new task,
	// then persist it. AutoStart tasks are then scheduled to run.
	e.emit(t)
	if opts.AutoStart {
		e.schedule()
	}
	return t.Snapshot(), nil
}

// Restore re-registers a persisted task (e.g. on startup) without auto-starting.
func (e *Engine) Restore(t *Task) {
	if t.Status == StatusDownloading || t.Status == StatusConnecting {
		t.Status = StatusPaused // we were not cleanly stopped
	}
	e.mu.Lock()
	e.tasks[t.ID] = &managed{task: t}
	e.order = append(e.order, t.ID)
	e.mu.Unlock()
}

// Start queues a paused/errored task for download.
func (e *Engine) Start(id string) error {
	e.mu.Lock()
	m, ok := e.tasks[id]
	e.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	switch m.task.getStatus() {
	case StatusDownloading, StatusConnecting, StatusCompleted:
		return nil
	}
	m.task.setStatus(StatusQueued, "")
	e.emitManaged(m)
	e.schedule()
	return nil
}

// Pause stops an active task; its progress is preserved for resume. It returns
// immediately: the status flips to Paused at once for instant UI feedback while
// the worker goroutine unwinds and flushes its resume metadata in the
// background.
func (e *Engine) Pause(id string) error {
	e.mu.Lock()
	m, ok := e.tasks[id]
	var cancel context.CancelFunc
	if ok {
		cancel = m.cancel
	}
	e.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	switch m.task.getStatus() {
	case StatusDownloading, StatusConnecting:
		if cancel != nil {
			cancel()
		}
		m.task.recalcDownloaded() // reflect current segment progress immediately
		m.task.mu.Lock()
		m.task.Speed = 0
		m.task.mu.Unlock()
		m.task.setStatus(StatusPaused, "")
		e.emitManaged(m)
	case StatusQueued:
		m.task.setStatus(StatusPaused, "")
		e.emitManaged(m)
	}
	return nil
}

// Remove cancels (if running) and deletes a task. It returns immediately; the
// worker is cancelled and file cleanup (when deleteFile is set) runs in the
// background once the worker has fully stopped, so the UI updates instantly.
func (e *Engine) Remove(id string, deleteFile bool) error {
	e.mu.Lock()
	m, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return ErrNotFound
	}
	m.removed = true
	delete(e.tasks, id)
	for i, oid := range e.order {
		if oid == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	cancel := m.cancel
	done := m.done
	running := m.running
	e.mu.Unlock()

	if running && cancel != nil {
		cancel()
	}
	e.cfg.OnRemoved(id)

	go func() {
		if running && done != nil {
			<-done // let the worker finish writing before we touch the files
		}
		if deleteFile {
			removeFinal(m.task.SavePath)
			removeMeta(m.task.SavePath)
			removePartial(m.task.SavePath)
		}
	}()
	return nil
}

// List returns snapshots of all tasks in insertion order.
func (e *Engine) List() []TaskInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]TaskInfo, 0, len(e.order))
	for _, id := range e.order {
		if m, ok := e.tasks[id]; ok {
			out = append(out, m.task.Snapshot())
		}
	}
	return out
}

// Get returns a single task snapshot.
func (e *Engine) Get(id string) (TaskInfo, error) {
	e.mu.Lock()
	m, ok := e.tasks[id]
	e.mu.Unlock()
	if !ok {
		return TaskInfo{}, ErrNotFound
	}
	return m.task.Snapshot(), nil
}

// Shutdown pauses all active tasks and prevents new ones from starting.
func (e *Engine) Shutdown() {
	e.mu.Lock()
	e.closed = true
	running := make([]*managed, 0)
	for _, m := range e.tasks {
		if m.running {
			running = append(running, m)
		}
	}
	e.mu.Unlock()
	for _, m := range running {
		if m.cancel != nil {
			m.cancel()
		}
	}
	for _, m := range running {
		<-m.done
	}
}

// schedule launches queued tasks until the concurrency limit is reached.
func (e *Engine) schedule() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	for _, id := range e.order {
		if e.activeCount >= e.cfg.MaxConcurrent {
			return
		}
		m := e.tasks[id]
		if m == nil || m.running {
			continue
		}
		if m.task.getStatus() != StatusQueued {
			continue
		}
		e.launchLocked(m)
	}
}

// launchLocked starts a task's worker. Caller must hold e.mu.
func (e *Engine) launchLocked(m *managed) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.running = true
	e.activeCount++
	go e.run(ctx, m)
}

// run performs the full download for one task and reschedules on completion.
func (e *Engine) run(ctx context.Context, m *managed) {
	t := m.task
	defer func() {
		e.mu.Lock()
		m.running = false
		m.cancel = nil
		e.activeCount--
		e.mu.Unlock()
		close(m.done)
		e.schedule()
	}()

	client := e.cfg.ClientFactory(t.Proxy)
	t.setStatus(StatusConnecting, "")
	e.emitManaged(m)

	// Resume path: a task we already know the layout of (paused→resumed in the
	// same session, restored from the DB, or with a sidecar .bdmeta) skips probing
	// and just refetches the remaining ranges. Fresh tasks take the fast-start path
	// below, which starts streaming bytes on the very first connection.
	resumable := e.loadResume(t)

	w, err := openPartFile(t.SavePath)
	if err != nil {
		e.fail(t, ctx, err)
		return
	}

	var xferErr error
	if resumable {
		t.setStatus(StatusDownloading, "")
		e.emitManaged(m)
		xferErr = e.transfer(ctx, client, t, w)
	} else {
		xferErr = e.fastStart(ctx, client, t, w, m)
	}
	if xferErr != nil {
		w.Close()
		_ = writeMeta(t)
		e.fail(t, ctx, xferErr)
		return
	}

	if err := finalize(w, t.SavePath); err != nil {
		e.fail(t, ctx, err)
		return
	}
	removeMeta(t.SavePath)
	t.recalcDownloaded()
	t.setStatus(StatusCompleted, "")
	e.emitManaged(m)
}

// loadResume reports whether the task can resume from a known segment layout
// (already in memory, or recovered from the sidecar .bdmeta file). When true the
// segments are populated and no network probe is needed; when false the task is
// a fresh download handled by fastStart.
func (e *Engine) loadResume(t *Task) bool {
	t.mu.RLock()
	hasSegments := len(t.Segments) > 0
	t.mu.RUnlock()
	if hasSegments {
		return true
	}

	if m, err := readMeta(t.SavePath); err == nil && m.TotalSize > 0 {
		t.mu.Lock()
		t.TotalSize = m.TotalSize
		t.Resumable = m.Resumable
		t.MIME = m.MIME
		if t.Filename == "" {
			t.Filename = m.Filename
		}
		t.Segments = make([]*Segment, len(m.Segments))
		for i := range m.Segments {
			s := m.Segments[i]
			t.Segments[i] = &s
		}
		t.mu.Unlock()
		t.recalcDownloaded()
		return true
	}
	return false
}

// fastStart downloads a fresh task with no pre-flight probe: it opens a single
// open-ended request (Range: bytes=0-) whose response headers reveal the size and
// range support, and whose body is streamed straight to disk as segment 0. The
// moment those headers arrive we know the total size, flip to Downloading and —
// if the server supports ranges — fan out the remaining segments on their own
// connections. This removes the dead round-trip that made downloads look stalled
// for the first few seconds, so they start at full speed like IDM.
func (e *Engine) fastStart(ctx context.Context, client *http.Client, t *Task, w *fileWriter, m *managed) error {
	headers := t.headersCopy()
	url := t.URL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	applyHeaders(req, headers)
	req.Header.Set("Range", "bytes=0-")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	// resp.Body is handed to the segment-0 streamer, which closes it.

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return fmt.Errorf("server returned %s", resp.Status)
	}

	total := int64(-1)
	ranged := false
	if resp.StatusCode == http.StatusPartialContent {
		ranged = true
		if n := parseContentRangeTotal(resp.Header.Get("Content-Range")); n > 0 {
			total = n
		} else if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
	} else { // 200 OK: server ignored the range request
		if strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") {
			ranged = true
		}
		if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
	}
	// Without a known size we cannot split into ranges; stream on one connection.
	if total <= 0 {
		ranged = false
	}

	t.mu.Lock()
	t.TotalSize = total
	t.Resumable = ranged
	if t.MIME == "" {
		t.MIME = resp.Header.Get("Content-Type")
	}
	if t.Filename == "" {
		t.Filename = resolveFilename(resp, url)
	}
	conns := t.Connections
	if !ranged {
		conns = 1
	}
	t.Segments = buildSegments(total, conns)
	segs := t.Segments
	t.mu.Unlock()

	// Size is known now, so progress, ETA and per-thread bars come alive at once.
	t.setStatus(StatusDownloading, "")
	e.emitManaged(m)

	var progress int64
	var wg sync.WaitGroup
	errCh := make(chan error, len(segs))

	// Segment 0 consumes the already-open response body — bytes are flowing from
	// the first packet, with no extra handshake.
	wg.Add(1)
	go func(s *Segment) {
		defer wg.Done()
		if err := streamSegment(ctx, resp, s, w, ranged, &progress); err != nil {
			errCh <- err
		}
	}(segs[0])

	// Remaining segments open their own ranged connections in parallel.
	for i := 1; i < len(segs); i++ {
		wg.Add(1)
		go func(s *Segment) {
			defer wg.Done()
			if err := downloadSegment(ctx, client, url, headers, s, w, ranged, &progress); err != nil {
				errCh <- err
			}
		}(segs[i])
	}

	stop := make(chan struct{})
	go e.reportProgress(t, &progress, stop)

	wg.Wait()
	close(stop)
	close(errCh)

	t.recalcDownloaded()
	_ = writeMeta(t)

	if err := ctx.Err(); err != nil {
		return err
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// transfer runs the segment workers concurrently and reports progress until all
// complete, the context is cancelled, or one fails.
func (e *Engine) transfer(ctx context.Context, client *http.Client, t *Task, w *fileWriter) error {
	t.mu.RLock()
	segs := t.Segments
	ranged := t.Resumable
	headers := t.headersCopy()
	url := t.URL
	t.mu.RUnlock()

	var progress int64
	atomic.StoreInt64(&progress, 0)

	var wg sync.WaitGroup
	errCh := make(chan error, len(segs))
	for _, seg := range segs {
		wg.Add(1)
		go func(s *Segment) {
			defer wg.Done()
			if err := downloadSegment(ctx, client, url, headers, s, w, ranged, &progress); err != nil {
				errCh <- err
			}
		}(seg)
	}

	stop := make(chan struct{})
	go e.reportProgress(t, &progress, stop)

	wg.Wait()
	close(stop)
	close(errCh)

	t.recalcDownloaded()
	_ = writeMeta(t)

	if err := ctx.Err(); err != nil {
		return err
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// reportProgress periodically computes speed and emits throttled updates. It
// emits one update immediately so the UI (total bar, per-thread bars and speed)
// comes alive the instant the transfer starts instead of after the first tick.
func (e *Engine) reportProgress(t *Task, progress *int64, stop <-chan struct{}) {
	const interval = 250 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastBytes int64
	lastTime := time.Now()
	// A lighter EMA (higher alpha) so the displayed speed converges to the real
	// rate in ~1s instead of feeling like it slowly creeps up over many seconds.
	const alpha = 0.6

	t.recalcDownloaded()
	if e.isActive(t.ID) {
		e.cfg.OnUpdate(t.Snapshot()) // immediate first paint
	}

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			cur := atomic.LoadInt64(progress)
			dt := now.Sub(lastTime).Seconds()
			if dt <= 0 {
				continue
			}
			instant := float64(cur-lastBytes) / dt
			lastBytes = cur
			lastTime = now

			t.mu.Lock()
			// Snap to the real rate on the first sample so the readout shows the
			// true speed immediately instead of creeping up from zero; smooth
			// thereafter.
			if t.Speed == 0 {
				t.Speed = int64(instant)
			} else {
				t.Speed = int64(alpha*instant + (1-alpha)*float64(t.Speed))
			}
			t.mu.Unlock()
			t.recalcDownloaded()
			if e.isActive(t.ID) {
				e.cfg.OnUpdate(t.Snapshot())
			}
		}
	}
}

func (e *Engine) fail(t *Task, ctx context.Context, err error) {
	t.mu.Lock()
	t.Speed = 0
	t.mu.Unlock()
	if ctx.Err() != nil {
		// Cancellation comes from the user (Pause/Remove/Shutdown), which has
		// already set the desired status (Paused, or Queued if resumed mid-unwind).
		// Don't clobber it — just persist the flushed progress.
	} else {
		t.setStatus(StatusError, err.Error())
	}
	e.emitIfActive(t)
}

// emit pushes both a UI update and a durable persist for a task.
func (e *Engine) emit(t *Task) {
	e.cfg.OnUpdate(t.Snapshot())
	e.cfg.OnPersist(t.Record())
}

func (e *Engine) emitManaged(m *managed) {
	e.mu.Lock()
	removed := m.removed
	e.mu.Unlock()
	if removed {
		return
	}
	e.emit(m.task)
}

func (e *Engine) emitIfActive(t *Task) {
	if !e.isActive(t.ID) {
		return
	}
	e.emit(t)
}

func (e *Engine) isActive(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.tasks[id]
	return m != nil && !m.removed
}

func (t *Task) headersCopy() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.Headers == nil {
		return nil
	}
	cp := make(map[string]string, len(t.Headers))
	for k, v := range t.Headers {
		cp[k] = v
	}
	return cp
}
