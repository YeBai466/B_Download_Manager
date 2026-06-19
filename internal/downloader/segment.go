package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var errStalled = errors.New("download stalled")

type transferPlan struct {
	workers int
	chunks  []*Chunk
	lanes   []*Segment
}

type transferOptions struct {
	Retries      int
	StallTimeout time.Duration
	Limiter      *speedLimiter
	Connections  *connectionLimiter
	Identity     responseIdentity
	Cancel       context.CancelFunc
}

type responseIdentity struct {
	ETag         string
	LastModified string
	FinalURL     string
	TotalSize    int64
}

type chunkQueue struct {
	mu     sync.Mutex
	chunks []*Chunk
	next   int
}

func newChunkQueue(chunks []*Chunk) *chunkQueue {
	return &chunkQueue{chunks: chunks}
}

func (q *chunkQueue) nextChunk() *Chunk {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.next < len(q.chunks) {
		c := q.chunks[q.next]
		q.next++
		if !c.Complete() {
			return c
		}
	}
	return nil
}

func downloadChunkWithRetry(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	headers map[string]string,
	chunk *Chunk,
	lane *Segment,
	w *fileWriter,
	progress *int64,
	opts transferOptions,
) error {
	if chunk.Complete() {
		return nil
	}
	retries := opts.Retries
	if retries < 1 {
		retries = defaultRetries
	}
	var last error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := chunk.Current()
		err := downloadChunk(ctx, client, rawURL, headers, chunk, lane, w, progress, opts)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isFatalDownloadError(err) {
			return err
		}
		if chunk.Current() > start {
			last = err
			continue
		}
		last = err
		if attempt < retries {
			delay := retryDelay(attempt)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("chunk %d failed after retries: %w", chunk.Index, last)
}

// downloadSegment is kept for older tests/helpers and the legacy transfer path.
// The engine's active path uses downloadChunkWithRetry and dynamic chunks.
func downloadSegment(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	headers map[string]string,
	seg *Segment,
	w *fileWriter,
	ranged bool,
	progress *int64,
) error {
	if ranged {
		chunk := &Chunk{Index: seg.Index, Start: seg.Start, End: seg.End, Downloaded: seg.loaded()}
		opts := transferOptions{Retries: defaultRetries, StallTimeout: defaultStallTimeout, Identity: responseIdentity{TotalSize: seg.End + 1}}
		return downloadChunkWithRetry(ctx, client, rawURL, headers, chunk, seg, w, progress, opts)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	applyHeaders(req, headers)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("expected 200 OK, got %s", resp.Status)
	}
	chunk := &Chunk{Index: seg.Index, Start: 0, End: seg.End, Downloaded: seg.loaded()}
	return streamOpenResponse(ctx, resp, chunk, seg, w, progress, transferOptions{StallTimeout: defaultStallTimeout}, seg.End >= 0)
}

// streamSegment is kept for the legacy fast-start path; new downloads use
// streamOpenResponse directly with Chunk state.
func streamSegment(
	ctx context.Context,
	resp *http.Response,
	seg *Segment,
	w *fileWriter,
	ranged bool,
	progress *int64,
) error {
	chunk := &Chunk{Index: seg.Index, Start: seg.Start, End: seg.End, Downloaded: seg.loaded()}
	return streamOpenResponse(ctx, resp, chunk, seg, w, progress, transferOptions{StallTimeout: defaultStallTimeout}, ranged && seg.End >= 0)
}

func downloadChunk(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	headers map[string]string,
	chunk *Chunk,
	lane *Segment,
	w *fileWriter,
	progress *int64,
	opts transferOptions,
) error {
	if opts.Connections != nil {
		if err := opts.Connections.Acquire(ctx); err != nil {
			return err
		}
		defer opts.Connections.Release()
	}

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	localOpts := opts
	localOpts.Cancel = cancel

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	applyHeaders(req, headers)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Current(), chunk.End))
	if opts.Identity.ETag != "" || opts.Identity.LastModified != "" {
		req.Header.Set("If-Range", ifRangeValue(opts.Identity))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return fatalError{err: fmt.Errorf("server rejected range for chunk %d: %s", chunk.Index, resp.Status)}
	}
	if resp.StatusCode != http.StatusPartialContent {
		return fatalError{err: fmt.Errorf("expected 206 Partial Content, got %s", resp.Status)}
	}
	if err := validatePartialResponse(resp, chunk.Current(), chunk.End, opts.Identity); err != nil {
		return fatalError{err: err}
	}
	return copyBody(reqCtx, resp.Body, chunk.Current(), chunk, lane, w, progress, localOpts, true)
}

func streamOpenResponse(
	ctx context.Context,
	resp *http.Response,
	chunk *Chunk,
	lane *Segment,
	w *fileWriter,
	progress *int64,
	opts transferOptions,
	capped bool,
) error {
	defer resp.Body.Close()
	return copyBody(ctx, resp.Body, chunk.Current(), chunk, lane, w, progress, opts, capped)
}

func copyBody(
	ctx context.Context,
	body io.Reader,
	offset int64,
	chunk *Chunk,
	lane *Segment,
	w *fileWriter,
	progress *int64,
	opts transferOptions,
	capped bool,
) error {
	buf := make([]byte, copyBufferSize(opts.Limiter))
	stall := opts.StallTimeout
	if stall <= 0 {
		stall = defaultStallTimeout
	}
	lastProgress := time.Now()
	var lastProgressUnix int64 = lastProgress.UnixNano()
	doneWatch := make(chan struct{})
	if opts.Cancel != nil {
		go func() {
			ticker := time.NewTicker(stall / 2)
			if stall/2 <= 0 {
				ticker = time.NewTicker(stall)
			}
			defer ticker.Stop()
			for {
				select {
				case <-doneWatch:
					return
				case <-ticker.C:
					last := time.Unix(0, atomic.LoadInt64(&lastProgressUnix))
					if time.Since(last) > stall {
						opts.Cancel()
						return
					}
				}
			}
		}()
		defer close(doneWatch)
	}
	for {
		if capped && chunk.Complete() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			now := time.Now()
			lastProgress = now
			atomic.StoreInt64(&lastProgressUnix, now.UnixNano())
			if capped {
				if rem := chunk.Remaining(); rem >= 0 && int64(n) > rem {
					n = int(rem)
				}
			}
			if opts.Limiter != nil {
				if err := opts.Limiter.Wait(ctx, n); err != nil {
					return err
				}
			}
			if _, werr := w.WriteAt(buf[:n], offset); werr != nil {
				return werr
			}
			offset += int64(n)
			chunk.add(int64(n))
			if lane != nil {
				lane.add(int64(n))
			}
			atomic.AddInt64(progress, int64(n))
			now = time.Now()
			lastProgress = now
			atomic.StoreInt64(&lastProgressUnix, now.UnixNano())
			if capped && chunk.Complete() {
				return nil
			}
		}
		if readErr == io.EOF {
			if capped && !chunk.Complete() {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if time.Since(lastProgress) > stall {
			return errStalled
		}
	}
}

// buildTransferPlan creates UI lanes and resumable chunks. The UI lane count is
// the worker count; chunks are smaller work units consumed dynamically.
func buildTransferPlan(totalSize int64, requested int) transferPlan {
	workers := smartConnections(totalSize, requested)
	if totalSize <= 0 {
		return transferPlan{
			workers: 1,
			chunks:  []*Chunk{{Index: 0, Start: 0, End: -1}},
			lanes:   []*Segment{{Index: 0, Start: 0, End: -1}},
		}
	}
	if int64(workers) > totalSize {
		workers = int(totalSize)
	}
	if workers < 1 {
		workers = 1
	}
	chunkSize := smartChunkSize(totalSize)
	chunks := make([]*Chunk, 0, (totalSize/chunkSize)+1)
	for start, idx := int64(0), 0; start < totalSize; idx++ {
		end := start + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		chunks = append(chunks, &Chunk{Index: idx, Start: start, End: end})
		start = end + 1
	}
	lanes := buildWorkerLanes(totalSize, workers)
	return transferPlan{workers: workers, chunks: chunks, lanes: lanes}
}

func buildWorkerLanes(totalSize int64, n int) []*Segment {
	if n < 1 {
		n = 1
	}
	lanes := make([]*Segment, n)
	end := totalSize - 1
	if totalSize <= 0 {
		end = -1
	}
	for i := 0; i < n; i++ {
		lanes[i] = &Segment{Index: i, Start: 0, End: end}
	}
	return lanes
}

// buildSegments splits totalSize into n UI lanes.
func buildSegments(totalSize int64, n int) []*Segment {
	if n < 1 {
		n = 1
	}
	if totalSize <= 0 {
		return []*Segment{{Index: 0, Start: 0, End: -1}}
	}
	if int64(n) > totalSize {
		n = int(totalSize)
		if n < 1 {
			n = 1
		}
	}
	segs := make([]*Segment, n)
	base := totalSize / int64(n)
	var start int64
	for i := 0; i < n; i++ {
		end := start + base - 1
		if i == n-1 {
			end = totalSize - 1
		}
		segs[i] = &Segment{Index: i, Start: start, End: end}
		start = end + 1
	}
	return segs
}

func validatePartialResponse(resp *http.Response, start, end int64, id responseIdentity) error {
	rs, re, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
	if !ok {
		return fmt.Errorf("missing or invalid Content-Range")
	}
	if rs != start || re != end {
		return fmt.Errorf("range mismatch: got %d-%d want %d-%d", rs, re, start, end)
	}
	if id.TotalSize > 0 && total > 0 && total != id.TotalSize {
		return fmt.Errorf("remote size changed: got %d want %d", total, id.TotalSize)
	}
	if id.ETag != "" && resp.Header.Get("ETag") != "" && resp.Header.Get("ETag") != id.ETag {
		return fmt.Errorf("remote ETag changed")
	}
	if id.LastModified != "" && resp.Header.Get("Last-Modified") != "" && resp.Header.Get("Last-Modified") != id.LastModified {
		return fmt.Errorf("remote Last-Modified changed")
	}
	return nil
}

func ifRangeValue(id responseIdentity) string {
	if id.ETag != "" {
		return id.ETag
	}
	return id.LastModified
}

type fatalError struct {
	err error
}

func (e fatalError) Error() string { return e.err.Error() }
func (e fatalError) Unwrap() error { return e.err }

func isFatalDownloadError(err error) bool {
	var fe fatalError
	return errors.As(err, &fe)
}

func retryDelay(attempt int) time.Duration {
	base := time.Duration(200*(1<<attempt)) * time.Millisecond
	jitter := time.Duration(rand.Intn(120)) * time.Millisecond
	if base > 2*time.Second {
		base = 2 * time.Second
	}
	return base + jitter
}

func copyBufferSize(l *speedLimiter) int {
	if l == nil {
		return 128 * 1024
	}
	l.mu.Lock()
	rate := l.rate
	l.mu.Unlock()
	if rate > 0 {
		return 16 * 1024
	}
	return 128 * 1024
}
