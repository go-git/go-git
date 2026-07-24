package http

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// trickleReader delivers one byte per Read after delay, up to n bytes, so a
// full window elapses well below any meaningful throughput floor.
type trickleReader struct {
	n     int
	delay time.Duration
}

func (r *trickleReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	r.n--
	p[0] = 'x'
	return 1, nil
}

func (r *trickleReader) Close() error { return nil }

func TestLowSpeedBody_AbortsStalledRead(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	b := newLowSpeedBody(pr, &LowSpeedGuard{Limit: 1, Time: 100 * time.Millisecond})

	done := make(chan error, 1)
	go func() {
		_, err := b.Read(make([]byte, 16))
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorContains(t, err, "transfer speed below")
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not honour the low-speed window")
	}
}

func TestLowSpeedBody_AbortsSlowTransfer(t *testing.T) {
	t.Parallel()

	b := newLowSpeedBody(&trickleReader{n: 100, delay: 20 * time.Millisecond}, &LowSpeedGuard{Limit: 1024 * 1024, Time: 100 * time.Millisecond})

	var err error
	buf := make([]byte, 1)
	for err == nil {
		_, err = b.Read(buf)
	}
	require.ErrorContains(t, err, "transfer speed below")
}

func TestLowSpeedBody_AllowsFastTransfer(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 4096)
	b := newLowSpeedBody(io.NopCloser(strings.NewReader(payload)), &LowSpeedGuard{Limit: 1, Time: time.Second})

	got, err := io.ReadAll(b)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
}

// TestDrainEnablesConnectionReuse pins the behaviour that
// httpResponseBody.DrainClose relies on: draining an unread response body to EOF
// returns the keep-alive connection to net/http's pool, whereas closing mid-body
// discards it.
func TestDrainEnablesConnectionReuse(t *testing.T) {
	t.Parallel()

	newConnsWith := func(t *testing.T, drain bool) int32 {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("x", 4096))
		}))
		var newConns atomic.Int32
		srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				newConns.Add(1)
			}
		}
		srv.Start()
		defer srv.Close()

		client := &http.Client{Transport: &http.Transport{}}
		do := func() {
			resp, err := client.Get(srv.URL)
			require.NoError(t, err)
			_, err = io.CopyN(io.Discard, resp.Body, 10)
			require.NoError(t, err)
			if drain {
				_, _ = io.Copy(io.Discard, resp.Body)
			}
			require.NoError(t, resp.Body.Close())
		}
		do()
		do()
		return newConns.Load()
	}

	t.Run("drain reuses the connection", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, int32(1), newConnsWith(t, true))
	})

	t.Run("no drain forfeits the connection", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, int32(2), newConnsWith(t, false))
	})
}
