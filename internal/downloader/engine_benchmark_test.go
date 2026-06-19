package downloader

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yebai/better-download-manager/internal/proxy"
)

func BenchmarkDownloadEngine(b *testing.B) {
	cases := []struct {
		name         string
		size         int
		supportRange bool
		connections  int
	}{
		{name: "small-range", size: 2 << 20, supportRange: true, connections: 8},
		{name: "large-range", size: 64 << 20, supportRange: true, connections: 8},
		{name: "no-range", size: 8 << 20, supportRange: false, connections: 8},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			data := makeData(tc.size)
			srv := benchRangeServer(data, tc.supportRange)
			defer srv.Close()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				updates := make(chan TaskInfo, 1024)
				e := NewEngine(Config{
					MaxConcurrent:  1,
					MaxConnections: tc.connections,
					ClientFactory:  func(proxy.Settings) *http.Client { return &http.Client{Timeout: 30 * time.Second} },
					OnUpdate: func(info TaskInfo) {
						select {
						case updates <- info:
						default:
						}
					},
				})
				dst := filepath.Join(b.TempDir(), "bench.bin")
				if _, err := e.Add(AddOptions{ID: "bench", URL: srv.URL, SavePath: dst, Connections: tc.connections, AutoStart: true}); err != nil {
					b.Fatal(err)
				}
				info := waitBenchDone(b, updates)
				if info.Status != StatusCompleted {
					b.Fatalf("status=%s err=%s", info.Status, info.Error)
				}
				e.Shutdown()
				_ = os.Remove(dst)
			}
		})
	}
}

func benchRangeServer(data []byte, supportRange bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end := 0, len(data)-1
		ranged := false
		if supportRange {
			w.Header().Set("Accept-Ranges", "bytes")
			if s, e, ok := parseRange(r.Header.Get("Range"), len(data)); ok {
				start, end, ranged = s, e, true
			}
		}
		body := data[start : end+1]
		w.Header().Set("Content-Length", itoa(len(body)))
		if ranged {
			w.Header().Set("Content-Range", "bytes "+itoa(start)+"-"+itoa(end)+"/"+itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(body)
	}))
}

func waitBenchDone(b *testing.B, updates <-chan TaskInfo) TaskInfo {
	b.Helper()
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	for {
		select {
		case info := <-updates:
			if info.Status == StatusCompleted || info.Status == StatusError {
				return info
			}
		case <-timer.C:
			b.Fatal("benchmark download timed out")
		}
	}
}
