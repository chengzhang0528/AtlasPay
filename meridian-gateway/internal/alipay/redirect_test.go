package alipay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

func TestParsePayURLBuildsOfficialPostForm(t *testing.T) {
	appPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	alipayPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(domain.AlipayCredentials{
		AppID:              "sandbox-app",
		PrivateKeyPEM:      privatePEM(appPrivate),
		AlipayPublicKeyPEM: publicPEM(&alipayPrivate.PublicKey),
	}, SandboxGatewayURL, "https://merchant.example/v1/webhooks/alipay")
	if err != nil {
		t.Fatal(err)
	}
	payURL, err := client.BuildPayURL("alipay.trade.page.pay", "pay-1", 1, "支付宝测试订单", "https://merchant.example/result")
	if err != nil {
		t.Fatal(err)
	}
	form, err := ParsePayURL(payURL)
	if err != nil {
		t.Fatal(err)
	}
	if form.Action != SandboxGatewayURL+"?charset=utf-8" {
		t.Fatalf("action=%q", form.Action)
	}
	fields := map[string]string{}
	for _, field := range form.Fields {
		fields[field.Name] = field.Value
	}
	if fields["method"] != "alipay.trade.page.pay" || fields["sign_type"] != "RSA2" || fields["sign"] == "" {
		t.Fatalf("unexpected fields: %v", fields)
	}
	if fields["biz_content"] == "" || fields["notify_url"] != "https://merchant.example/v1/webhooks/alipay" {
		t.Fatalf("missing business fields: %v", fields)
	}
	if _, exists := fields["charset"]; exists {
		t.Fatalf("charset must only be sent in the form action: %v", fields)
	}
}

func TestParsePayURLRejectsUntrustedGatewayAndMethod(t *testing.T) {
	for _, value := range []string{
		"https://example.com/gateway.do?app_id=x&method=alipay.trade.page.pay&biz_content=x&charset=utf-8&sign_type=RSA2&sign=x",
		SandboxGatewayURL + "?app_id=x&method=alipay.trade.precreate&biz_content=x&charset=utf-8&sign_type=RSA2&sign=x",
		SandboxGatewayURL + "?app_id=x&method=alipay.trade.page.pay&biz_content=x&charset=utf-8&sign_type=RSA2&sign=x&sign=y",
	} {
		if _, err := ParsePayURL(value); err == nil {
			t.Fatalf("ParsePayURL(%q) should fail", value)
		}
	}
}

func TestParsePayURLRejectsMissingOrUnsupportedCharset(t *testing.T) {
	for _, value := range []string{
		SandboxGatewayURL + "?app_id=x&method=alipay.trade.page.pay&biz_content=x&sign_type=RSA2&sign=x",
		SandboxGatewayURL + "?app_id=x&method=alipay.trade.page.pay&biz_content=x&charset=gbk&sign_type=RSA2&sign=x",
	} {
		if _, err := ParsePayURL(value); err == nil {
			t.Fatalf("ParsePayURL(%q) should fail", value)
		}
	}
}

func TestHTTPPayRedirecterPostsFormAndReturnsOfficialNextHop(t *testing.T) {
	var capturedMethod string
	var capturedURL string
	var capturedValues url.Values
	var capturedUserAgent string
	var capturedLanguage string
	transport := redirectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		capturedMethod = request.Method
		capturedURL = request.URL.String()
		capturedValues, err = url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		capturedUserAgent = request.Header.Get("User-Agent")
		capturedLanguage = request.Header.Get("Accept-Language")
		return redirectResponse(request, http.StatusFound, "https://unitradeprod-sandbox.dl.alipaydev.com/appAssign.htm?session=test"), nil
	})
	redirecter := NewHTTPPayRedirecter(transport)
	form := PayForm{Action: SandboxGatewayURL + "?charset=utf-8", Fields: []PayFormField{{Name: "method", Value: "alipay.trade.page.pay"}, {Name: "sign", Value: "signed"}}}
	location, err := redirecter.ResolvePayRedirect(context.Background(), form, PayRedirectMetadata{UserAgent: "Meridian-Test", AcceptLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if location.String() != "https://unitradeprod-sandbox.dl.alipaydev.com/appAssign.htm?session=test" {
		t.Fatalf("location=%q", location)
	}
	if capturedMethod != http.MethodPost || capturedURL != form.Action || capturedValues.Get("method") != "alipay.trade.page.pay" || capturedValues.Get("sign") != "signed" {
		t.Fatalf("unexpected request: method=%s url=%s values=%v", capturedMethod, capturedURL, capturedValues)
	}
	if capturedUserAgent != "Meridian-Test" || capturedLanguage != "zh-CN" {
		t.Fatalf("browser metadata not forwarded: user_agent=%q language=%q", capturedUserAgent, capturedLanguage)
	}
}

func TestHTTPPayRedirecterRejectsUntrustedOrInvalidRedirect(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		location string
	}{
		{name: "untrusted host", status: http.StatusFound, location: "https://example.com/steal"},
		{name: "lookalike host", status: http.StatusFound, location: "https://alipaydev.com.evil.example/steal"},
		{name: "insecure scheme", status: http.StatusFound, location: "http://auth.alipay.com/login"},
		{name: "userinfo", status: http.StatusFound, location: "https://user@auth.alipay.com/login"},
		{name: "custom port", status: http.StatusFound, location: "https://auth.alipay.com:444/login"},
		{name: "non redirect response", status: http.StatusOK, location: "https://auth.alipay.com/login"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := redirectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return redirectResponse(request, test.status, test.location), nil
			})
			form := PayForm{Action: ProductionGatewayURL + "?charset=utf-8", Fields: []PayFormField{{Name: "sign", Value: "signed"}}}
			if _, err := NewHTTPPayRedirecter(transport).ResolvePayRedirect(context.Background(), form, PayRedirectMetadata{}); err == nil {
				t.Fatal("untrusted or invalid redirect should fail")
			}
		})
	}

	requestCount := 0
	transport := redirectRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		return redirectResponse(request, http.StatusFound, "https://auth.alipay.com/login"), nil
	})
	invalidForms := []PayForm{
		{Action: "https://example.com/gateway.do?charset=utf-8", Fields: []PayFormField{{Name: "sign", Value: "signed"}}},
		{Action: SandboxGatewayURL + "?charset=utf-8", Fields: []PayFormField{{Name: "sign", Value: "one"}, {Name: "sign", Value: "two"}}},
	}
	for _, form := range invalidForms {
		if _, err := NewHTTPPayRedirecter(transport).ResolvePayRedirect(context.Background(), form, PayRedirectMetadata{}); err == nil {
			t.Fatalf("invalid form should fail: %+v", form)
		}
	}
	if requestCount != 0 {
		t.Fatalf("invalid form made %d outbound requests", requestCount)
	}
}

type redirectRoundTripFunc func(*http.Request) (*http.Response, error)

func (function redirectRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func redirectResponse(request *http.Request, status int, location string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Location": []string{location}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}
}
