package downloader

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// ProbeResult holds the metadata discovered about a download target.
type ProbeResult struct {
	TotalSize    int64 // -1 when unknown
	Resumable    bool  // server honours byte ranges
	Filename     string
	MIME         string
	FinalURL     string // after redirects
	ETag         string
	LastModified string
}

// Probe is the exported entry point used by the service layer to preview a URL
// before adding it as a task.
func Probe(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*ProbeResult, error) {
	return probe(ctx, client, rawURL, headers)
}

// probe issues a ranged GET (Range: bytes=0-0) to learn the size, range
// support, filename and content type without downloading the body. A ranged GET
// is more reliable than HEAD, which many servers handle incorrectly.
func probe(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*ProbeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	applyHeaders(req, headers)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	res := &ProbeResult{
		TotalSize:    -1,
		MIME:         resp.Header.Get("Content-Type"),
		FinalURL:     resp.Request.URL.String(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	// A 206 with Content-Range means ranges are supported and gives total size.
	if resp.StatusCode == http.StatusPartialContent {
		res.Resumable = true
		if total := parseContentRangeTotal(resp.Header.Get("Content-Range")); total > 0 {
			res.TotalSize = total
		}
	} else {
		// 200 OK means the server ignored the range request. Some servers still
		// advertise Accept-Ranges incorrectly, so do not segment unless we see a
		// real 206 response.
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
				res.TotalSize = n
			}
		}
	}

	// Unknown size cannot be segmented even if ranges are advertised.
	if res.TotalSize <= 0 {
		res.Resumable = false
	}

	res.Filename = resolveFilename(resp, rawURL)
	return res, nil
}

// resolveFilename derives a filename from Content-Disposition, falling back to
// the URL path, then a generic default.
func resolveFilename(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := params["filename*"]; fn != "" {
				if dec := decodeRFC5987(fn); dec != "" {
					return sanitizeFilename(dec)
				}
			}
			if fn := params["filename"]; fn != "" {
				return sanitizeFilename(fn)
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			if name, err := url.PathUnescape(base); err == nil {
				return sanitizeFilename(name)
			}
			return sanitizeFilename(base)
		}
	}
	return "download"
}

func decodeRFC5987(v string) string {
	// Format: UTF-8''<percent-encoded>
	parts := strings.SplitN(v, "''", 2)
	if len(parts) != 2 {
		return ""
	}
	if dec, err := url.QueryUnescape(parts[1]); err == nil {
		return dec
	}
	return ""
}

var invalidFilenameChars = strings.NewReplacer(
	"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
	"\"", "_", "<", "_", ">", "_", "|", "_",
)

// sanitizeFilename strips path separators and characters illegal on Windows.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	name = invalidFilenameChars.Replace(name)
	if name == "" {
		return "download"
	}
	return name
}

func parseContentRangeTotal(cr string) int64 {
	// e.g. "bytes 0-0/123456"
	idx := strings.LastIndex(cr, "/")
	if idx < 0 {
		return -1
	}
	totalStr := strings.TrimSpace(cr[idx+1:])
	if totalStr == "*" {
		return -1
	}
	n, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func parseContentRange(cr string) (start, end, total int64, ok bool) {
	cr = strings.TrimSpace(cr)
	if !strings.HasPrefix(strings.ToLower(cr), "bytes ") {
		return 0, 0, 0, false
	}
	rest := strings.TrimSpace(cr[len("bytes "):])
	slash := strings.LastIndex(rest, "/")
	dash := strings.Index(rest, "-")
	if slash < 0 || dash < 0 || dash > slash {
		return 0, 0, 0, false
	}
	var err error
	start, err = strconv.ParseInt(strings.TrimSpace(rest[:dash]), 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	end, err = strconv.ParseInt(strings.TrimSpace(rest[dash+1:slash]), 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	totalStr := strings.TrimSpace(rest[slash+1:])
	if totalStr == "*" {
		return start, end, -1, true
	}
	total, err = strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

func applyHeaders(req *http.Request, headers map[string]string) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) BetterDownloadManager/1.0"
