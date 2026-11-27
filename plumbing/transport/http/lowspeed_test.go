package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

func TestLowSpeedWindow(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		totals    []int64
		expiredAt int
	}{
		{name: "stall", totals: []int64{0, 0, 0}, expiredAt: 2},
		{name: "slow trickle", totals: []int64{0, 10, 20}, expiredAt: 2},
		{name: "threshold is allowed", totals: []int64{0, 100, 200, 300}, expiredAt: -1},
		{name: "recovery resets the period", totals: []int64{0, 0, 400, 400, 400, 400, 400, 400}, expiredAt: 7},
		{name: "old progress expires", totals: []int64{0, 800, 800, 800, 800, 800, 800, 800, 800}, expiredAt: 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var window lowSpeedWindow
			start := time.Unix(100, 0)
			guard := LowSpeedGuard{Limit: 100, Time: 2 * time.Second}
			for second, total := range tt.totals {
				got := window.expired(start.Add(time.Duration(second)*time.Second), total, guard)
				require.Equal(t, second == tt.expiredAt, got, "second %d", second)
			}
		})
	}
}

func TestLowSpeedRequests(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"discovery headers", "discovery body", "error body", "POST headers", "POST body"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			stalled := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.Method == http.MethodGet && strings.HasPrefix(phase, "POST") {
					w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
					_, _ = fmt.Fprint(w, v2Advertisement)
					return
				}
				if phase == "error body" {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusInternalServerError)
				} else if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				}
				if strings.HasSuffix(phase, "body") {
					w.(http.Flusher).Flush()
				}
				close(stalled)
				<-r.Context().Done()
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			session, err := handshakeFor(t, srv.URL, clone{ctx: ctx}, Options{
				Client:   srv.Client(),
				LowSpeed: &LowSpeedGuard{Limit: 1, Time: 100 * time.Millisecond},
			})
			if err == nil {
				defer func() { _ = session.Close() }()
				_, err = session.GetRemoteRefs(ctx, &transport.GetRemoteRefsOptions{})
			}
			require.ErrorContains(t, err, "transfer speed below")
			require.NoError(t, ctx.Err(), "the low-speed guard must expire before the caller's deadline")
			select {
			case <-stalled:
			default:
				t.Fatal("the request did not reach the selected stall")
			}
		})
	}
}

func TestLowSpeedHealthyUpload(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		original := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			buf := make([]byte, 100)
			for {
				_, err := req.Body.Read(buf)
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, err
				}
				select {
				case <-time.After(250 * time.Millisecond):
				case <-req.Context().Done():
					return nil, context.Cause(req.Context())
				}
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})}
		client := NewTransport(Options{
			Client: original, LowSpeed: &LowSpeedGuard{Limit: 100, Time: time.Second},
		}).resolveClient()

		start := time.Now()
		resp, err := client.Post("http://example.test/", "application/octet-stream", strings.NewReader(strings.Repeat("x", 2000)))
		require.NoError(t, err)
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "ok", string(got))
		require.Greater(t, time.Since(start), time.Second)
	})
}

func TestLowSpeedRequestReplay(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		original := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			require.NotNil(t, req.GetBody)
			replay, err := req.GetBody()
			require.NoError(t, err)
			defer replay.Close()
			buf := make([]byte, 100)
			for range 8 {
				_, err := io.ReadFull(replay, buf)
				require.NoError(t, err)
				time.Sleep(250 * time.Millisecond)
				require.NoError(t, req.Context().Err())
			}
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})}
		client := NewTransport(Options{
			Client: original, LowSpeed: &LowSpeedGuard{Limit: 100, Time: time.Second},
		}).resolveClient()
		req, err := http.NewRequest(http.MethodPost, "http://example.test/", strings.NewReader(strings.Repeat("x", 800)))
		require.NoError(t, err)
		body := req.Body
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, body, req.Body, "the wrapper must not replace the caller's body")
		require.NoError(t, req.Context().Err())
	})
}

func TestLowSpeedCleanup(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"EOF", "early close", "read error", "request error", "parent cancellation", "idle timeout"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				failure := errors.New("transport failed")
				var closes atomic.Int64
				original := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					switch outcome {
					case "request error":
						return nil, failure
					case "parent cancellation":
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					var reader io.Reader = strings.NewReader("ok")
					if outcome == "read error" {
						reader = iotest.ErrReader(failure)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: &closeTrackingBody{
							ReadCloser: io.NopCloser(reader),
							closed:     &closes,
						},
					}, nil
				})}
				client := NewTransport(Options{
					Client: original, LowSpeed: &LowSpeedGuard{Limit: 1, Time: time.Second},
				}).resolveClient()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if outcome == "parent cancellation" {
					go func() {
						time.Sleep(100 * time.Millisecond)
						cancel()
					}()
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test/", nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				if outcome == "parent cancellation" {
					require.ErrorIs(t, err, context.Canceled)
					return
				}
				if outcome == "request error" {
					require.ErrorIs(t, err, failure)
					return
				}
				require.NoError(t, err)
				defer resp.Body.Close()
				if outcome == "EOF" || outcome == "read error" {
					_, err := io.ReadAll(resp.Body)
					if outcome == "EOF" {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, failure)
					}
				}
				if outcome == "early close" {
					require.NoError(t, resp.Body.Close())
				}
				time.Sleep(2 * time.Second)
				synctest.Wait()
				var slow *lowSpeedError
				if outcome == "idle timeout" {
					require.ErrorAs(t, context.Cause(resp.Request.Context()), &slow)
					require.ErrorIs(t, slow, context.DeadlineExceeded)
					require.True(t, slow.Timeout())
				} else {
					require.NotErrorAs(t, context.Cause(resp.Request.Context()), &slow)
				}
				closed := make(chan error, 2)
				for range 2 {
					go func() { closed <- resp.Body.Close() }()
				}
				require.NoError(t, <-closed)
				require.NoError(t, <-closed)
				require.Equal(t, int64(1), closes.Load())
			})
		})
	}
}

func TestLowSpeedRedirectAndReauthentication(t *testing.T) {
	t.Parallel()
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%t", retry), func(t *testing.T) {
			t.Parallel()
			origin, destination, seen := redirectPair(t, http.StatusTemporaryRedirect, func(w http.ResponseWriter, r *http.Request) {
				if retry && r.Header.Get("Authorization") == "" {
					challenge(w)
					return
				}
				<-r.Context().Done()
			})
			hook := newHook("destination-token", destination)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := handshakeFor(t, origin, clone{ctx: ctx}, Options{
				Credentials: hook.fn,
				LowSpeed:    &LowSpeedGuard{Limit: 1, Time: 50 * time.Millisecond},
			})
			var slow *lowSpeedError
			require.ErrorAs(t, err, &slow)
			require.NoError(t, ctx.Err())
			require.NotErrorIs(t, err, transport.ErrAuthenticationRequired)
			if retry {
				require.Len(t, seen.all(), 2)
			} else {
				require.Len(t, seen.all(), 1)
			}
		})
	}
}

func TestLowSpeedHTTP2Isolation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if r.URL.Path == "/slow" {
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	srv.EnableHTTP2 = true
	var connections atomic.Int64
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	srv.StartTLS()
	defer srv.Close()

	client := NewTransport(Options{
		Client: srv.Client(), LowSpeed: &LowSpeedGuard{Limit: 1, Time: 200 * time.Millisecond},
	}).resolveClient()
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/slow", nil)
	require.NoError(t, err)
	slow, err := client.Do(req)
	require.NoError(t, err)
	defer slow.Body.Close()
	<-started
	for i := range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/fast", nil)
		require.NoError(t, err)
		fast, err := client.Do(req)
		require.NoError(t, err)
		got, err := io.ReadAll(fast.Body)
		require.NoError(t, fast.Body.Close())
		require.NoError(t, err)
		require.Equal(t, "ok", string(got))
		if i == 0 {
			_, err := io.ReadAll(slow.Body)
			var timeout *lowSpeedError
			require.ErrorAs(t, err, &timeout)
		}
	}
	require.Equal(t, int64(1), connections.Load())
}

func TestLowSpeedDownload(t *testing.T) {
	t.Parallel()
	for _, size := range []int{1, 100} {
		t.Run(fmt.Sprintf("chunk=%d", size), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				original := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					reader, writer := io.Pipe()
					stop := context.AfterFunc(req.Context(), func() {
						_ = reader.CloseWithError(context.Cause(req.Context()))
					})
					done := make(chan struct{})
					t.Cleanup(func() { <-done })
					go func() {
						defer close(done)
						defer writer.Close()
						defer stop()
						for range 20 {
							time.Sleep(100 * time.Millisecond)
							if _, err := io.WriteString(writer, strings.Repeat("x", size)); err != nil {
								return
							}
						}
					}()
					return &http.Response{StatusCode: http.StatusOK, Body: reader}, nil
				})}
				client := NewTransport(Options{
					Client: original, LowSpeed: &LowSpeedGuard{Limit: 100, Time: time.Second},
				}).resolveClient()
				resp, err := client.Get("http://example.test/")
				require.NoError(t, err)
				defer resp.Body.Close()
				data, err := io.ReadAll(resp.Body)
				if size == 1 {
					var timeout *lowSpeedError
					require.ErrorAs(t, err, &timeout)
				} else {
					require.NoError(t, err)
					require.Len(t, data, 20*size)
				}
			})
		})
	}
}

func TestLowSpeedKeepsConnection(t *testing.T) {
	t.Parallel()
	srv, connections := connCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_, _ = io.WriteString(w, v2Advertisement)
		} else {
			_, _ = io.WriteString(w, pkt(testSHA+" refs/heads/main\n")+"0000")
		}
	})
	session, err := handshakeAt(t, srv.URL, Options{
		Client: srv.Client(), LowSpeed: &LowSpeedGuard{Limit: 1, Time: time.Second},
	})
	require.NoError(t, err)
	defer session.Close()
	for range 5 {
		refs, err := session.GetRemoteRefs(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, refs.References, 1)
	}
	require.Equal(t, int64(1), connections.Load())
}

func TestLowSpeedClientTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		client := NewTransport(Options{
			Client: &http.Client{
				Timeout: 100 * time.Millisecond,
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}),
			},
			LowSpeed: &LowSpeedGuard{Limit: 1, Time: time.Second},
		}).resolveClient()
		_, err := client.Get("http://example.test/")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		var low *lowSpeedError
		require.NotErrorAs(t, err, &low)
	})
}

func TestLowSpeedGitCompatibility(t *testing.T) {
	t.Parallel()
	requireGitV2(t)

	for _, phase := range []string{"discovery body", "POST headers", "POST body", "healthy"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
					if phase != "discovery body" {
						_, _ = io.WriteString(w, pkt("version 2\n")+pkt("ls-refs\n")+pkt("fetch\n")+"0000")
						return
					}
				}
				if phase == "healthy" {
					_, _ = io.WriteString(w, pkt(testSHA+" refs/heads/main\n")+"0000")
					return
				}
				if strings.HasSuffix(phase, "body") {
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := handshakeFor(t, srv.URL, clone{ctx: ctx}, Options{
				LowSpeed: &LowSpeedGuard{Limit: 1000, Time: time.Second},
			})
			if err == nil {
				defer session.Close()
				_, err = session.GetRemoteRefs(ctx, nil)
			}
			if phase == "healthy" {
				require.NoError(t, err)
			} else {
				var timeout *lowSpeedError
				require.ErrorAs(t, err, &timeout)
			}

			cmd := gitenv.CommandContext(ctx, "git", "-c", "protocol.version=2",
				"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=1",
				"ls-remote", srv.URL+"/repo.git")
			out, err := cmd.CombinedOutput()
			if phase == "healthy" {
				require.NoError(t, err, "%s", out)
				require.Contains(t, string(out), testSHA)
			} else {
				require.Error(t, err)
				require.Contains(t, string(out), "Operation too slow")
			}
			require.NoError(t, ctx.Err())
		})
	}
}
