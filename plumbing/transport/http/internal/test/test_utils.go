package test

import (
	"encoding/base64"
	"strings"
	"sync/atomic"

	"github.com/elazarl/goproxy"
)

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
