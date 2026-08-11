package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver/koko/pkg/config"
)

func normalizeHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(value, "[]")
}

func hostMatches(allowedHost, originHost string) bool {
	allowedName := normalizeHost(allowedHost)
	originName := normalizeHost(originHost)
	return allowedName != "" && originName != "" && allowedName == originName
}

func isInternalHost(host string) bool {
	host = normalizeHost(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func domainsAllowOrigin(originHost, domains string) bool {
	for _, domain := range strings.Split(domains, ",") {
		domain = strings.TrimSpace(domain)
		if domain == "*" || hostMatches(domain, originHost) {
			return true
		}
	}
	return false
}

func OriginAllowed(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	originURL, err := url.Parse(origin)
	if err != nil || originURL.Host == "" {
		return false
	}
	originHost := normalizeHost(originURL.Host)
	return hostMatches(originHost, request.Host) || isInternalHost(originHost) ||
		domainsAllowOrigin(originHost, config.GetConf().DOMAINS)
}

func CORS() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")
		if origin == "" {
			ctx.Next()
			return
		}
		if !OriginAllowed(ctx.Request) {
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}

		ctx.Header("Access-Control-Allow-Origin", origin)
		ctx.Header("Access-Control-Allow-Credentials", "true")
		ctx.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		ctx.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Date, X-CSRFToken, X-JMS-ORG, X-Koko-Connect-Ticket")
		ctx.Header("Access-Control-Expose-Headers", "Content-Disposition")
		ctx.Header("Vary", "Origin")
		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}
		ctx.Next()
	}
}
