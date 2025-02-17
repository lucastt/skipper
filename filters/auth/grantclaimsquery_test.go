package auth_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/filters"
	"github.com/zalando/skipper/filters/auth"
	"github.com/zalando/skipper/proxy/proxytest"
)

func TestGrantClaimsQuery(t *testing.T) {
	provider := newGrantTestAuthServer(testToken, testAccessCode)
	defer provider.Close()

	tokeninfo := newGrantTestTokeninfo(testToken, "{\"scope\":[\"match\"], \"uid\":\"foo\"}")
	defer tokeninfo.Close()

	config := newGrantTestConfig(tokeninfo.URL, provider.URL)

	client := newGrantHTTPClient()

	cookie, err := newGrantCookie(*config)
	if err != nil {
		t.Fatal(err)
	}

	createProxyForQuery := func(config *auth.OAuthConfig, query string) *proxytest.TestProxy {
		proxy, err := newAuthProxy(config, &eskip.Route{
			Filters: []*eskip.Filter{
				{Name: filters.OAuthGrantName},
				{Name: filters.GrantClaimsQueryName, Args: []interface{}{query}},
				{Name: filters.StatusName, Args: []interface{}{http.StatusNoContent}},
			},
			BackendType: eskip.ShuntBackend,
		})
		if err != nil {
			t.Fatal(err)
		}
		return proxy
	}

	t.Run("check that matching tokeninfo properties allows the request", func(t *testing.T) {
		proxy := createProxyForQuery(config, "/allowed:scope.#[==\"match\"]")
		defer proxy.Close()

		url := fmt.Sprint(proxy.URL, "/allowed")
		rsp := grantQueryWithCookie(t, client, url, cookie)

		checkStatus(t, rsp, http.StatusNoContent)
	})

	t.Run("check that non-matching tokeninfo properties block the request", func(t *testing.T) {
		proxy := createProxyForQuery(config, "/forbidden:scope.#[==\"noMatch\"]")
		defer proxy.Close()

		url := fmt.Sprint(proxy.URL, "/forbidden")
		rsp := grantQueryWithCookie(t, client, url, cookie)

		checkStatus(t, rsp, http.StatusUnauthorized)
	})

	t.Run("check that the subject claim gets initialized from a configurable tokeninfo property and is queriable", func(t *testing.T) {
		newConfig := *config
		newConfig.TokeninfoSubjectKey = "uid"

		proxy := createProxyForQuery(&newConfig, "/allowed:@_:sub%\"foo\"")
		defer proxy.Close()

		url := fmt.Sprint(proxy.URL, "/allowed")
		rsp := grantQueryWithCookie(t, client, url, cookie)

		checkStatus(t, rsp, http.StatusNoContent)
	})
}
