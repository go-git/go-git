package test

import (
	"crypto/tls"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/elazarl/goproxy"

	. "gopkg.in/check.v1"
)

//go:embed testdata/certs/*
var certs embed.FS

// Make sure you close the server after the test.
func SetupProxyServer(c *C, handler http.Handler, isTls, schemaAddr bool) (string, *http.Server, net.Listener) {
	httpListener, err := net.Listen("tcp", ":0")
	c.Assert(err, IsNil)

	schema := "http"
	if isTls {
		schema = "https"
	}

	addr := "localhost:%d"
	if schemaAddr {
		addr = schema + "://localhost:%d"
	}

	httpProxyAddr := fmt.Sprintf(addr, httpListener.Addr().(*net.TCPAddr).Port)
	proxyServer := http.Server{
		Addr:    httpProxyAddr,
		Handler: handler,
	}
	if isTls {
		certf, err := certs.Open("testdata/certs/server.crt")
		c.Assert(err, IsNil)
		defer certf.Close()
		keyf, err := certs.Open("testdata/certs/server.key")
		c.Assert(err, IsNil)
		defer keyf.Close()
		cert, err := io.ReadAll(certf)
		c.Assert(err, IsNil)
		key, err := io.ReadAll(keyf)
		c.Assert(err, IsNil)
		keyPair, err := tls.X509KeyPair(cert, key)
		c.Assert(err, IsNil)
		cfg := &tls.Config{
			NextProtos:   []string{"http/1.1"},
			Certificates: []tls.Certificate{keyPair},
		}

		// Due to how golang manages http/2 when provided with custom TLS config,
		// servers and clients running in the same process leads to issues.
		// Ref: https://github.com/golang/go/issues/21336
		proxyServer.TLSConfig = cfg
	}

	go func() {
		var err error
		if isTls {
			err = proxyServer.ServeTLS(httpListener, "", "")
		} else {
			err = proxyServer.Serve(httpListener)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	return httpProxyAddr, &proxyServer, httpListener
}

func SetupHTTPProxy(proxy *goproxy.ProxyHttpServer, proxiedRequests *int32) {
	// The request is being forwarded to the local test git server in this handler.
	var proxyHandler goproxy.FuncReqHandler = func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.Contains(req.Host, "localhost") {
			user, pass, _ := ParseBasicAuth(req.Header.Get("Proxy-Authorization"))
			if user != "user" || pass != "pass" {
				return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusUnauthorized, "")
			}
			atomic.AddInt32(proxiedRequests, 1)
			return req, nil
		}
		// Reject if it isn't our request.
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusForbidden, "")
	}
	proxy.OnRequest().Do(proxyHandler)
}

func SetupHTTPSProxy(proxy *goproxy.ProxyHttpServer, proxiedRequests *int32) {
	var proxyHandler goproxy.FuncHttpsHandler = func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if strings.Contains(host, "github.com") {
			user, pass, _ := ParseBasicAuth(ctx.Req.Header.Get("Proxy-Authorization"))
			if user != "user" || pass != "pass" {
				return goproxy.RejectConnect, host
			}
			atomic.AddInt32(proxiedRequests, 1)
			return goproxy.OkConnect, host
		}
		// Reject if it isn't our request.
		return goproxy.RejectConnect, host
	}
	proxy.OnRequest().HandleConnect(proxyHandler)
}

// adapted from https://github.com/golang/go/blob/2ef70d9d0f98832c8103a7968b195e560a8bb262/src/net/http/request.go#L959
func ParseBasicAuth(auth string) (username, password string, ok bool) {
	const prefix = "Basic "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", "", false
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return "", "", false
	}
	cs := string(c)
	username, password, ok = strings.Cut(cs, ":")
	if !ok {
		return "", "", false
	}
	return username, password, true
}
