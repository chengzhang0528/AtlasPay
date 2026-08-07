package alipay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type PayFormField struct {
	Name  string
	Value string
}

type PayForm struct {
	Action string
	Fields []PayFormField
}

type PayRedirectMetadata struct {
	UserAgent      string
	AcceptLanguage string
}

type PayRedirecter interface {
	ResolvePayRedirect(context.Context, PayForm, PayRedirectMetadata) (*url.URL, error)
}

type HTTPPayRedirecter struct {
	client *http.Client
}

func NewHTTPPayRedirecter(transport http.RoundTripper) *HTTPPayRedirecter {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &HTTPPayRedirecter{client: &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func ParsePayURL(value string) (PayForm, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "https" || parsed.Port() != "" || parsed.Path != "/gateway.do" {
		return PayForm{}, errors.New("invalid Alipay pay URL")
	}
	action := ""
	switch {
	case strings.EqualFold(parsed.Hostname(), "openapi.alipay.com"):
		action = ProductionGatewayURL
	case strings.EqualFold(parsed.Hostname(), "openapi-sandbox.dl.alipaydev.com"):
		action = SandboxGatewayURL
	default:
		return PayForm{}, errors.New("unsupported Alipay gateway")
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return PayForm{}, errors.New("invalid Alipay pay parameters")
	}
	charset := values.Get("charset")
	if !strings.EqualFold(charset, "utf-8") {
		return PayForm{}, errors.New("unsupported Alipay pay charset")
	}
	if values.Get("sign_type") != "RSA2" || values.Get("sign") == "" || values.Get("app_id") == "" || values.Get("biz_content") == "" {
		return PayForm{}, errors.New("incomplete Alipay pay parameters")
	}
	method := values.Get("method")
	if method != "alipay.trade.page.pay" && method != "alipay.trade.wap.pay" {
		return PayForm{}, errors.New("unsupported Alipay pay method")
	}
	keys := make([]string, 0, len(values))
	for key, all := range values {
		if len(all) != 1 {
			return PayForm{}, errors.New("ambiguous Alipay pay parameter")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]PayFormField, 0, len(keys)-1)
	for _, key := range keys {
		if key == "charset" {
			continue
		}
		fields = append(fields, PayFormField{Name: key, Value: values.Get(key)})
	}
	return PayForm{Action: action + "?charset=" + url.QueryEscape(charset), Fields: fields}, nil
}

func (redirecter *HTTPPayRedirecter) ResolvePayRedirect(ctx context.Context, form PayForm, metadata PayRedirectMetadata) (*url.URL, error) {
	if redirecter == nil || redirecter.client == nil || (form.Action != ProductionGatewayURL+"?charset=utf-8" && form.Action != SandboxGatewayURL+"?charset=utf-8") {
		return nil, errors.New("invalid Alipay redirect request")
	}
	values := url.Values{}
	for _, field := range form.Fields {
		if field.Name == "" || len(values[field.Name]) != 0 {
			return nil, errors.New("invalid Alipay redirect fields")
		}
		values.Add(field.Name, field.Value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, form.Action, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, errors.New("build Alipay redirect request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if metadata.UserAgent != "" {
		request.Header.Set("User-Agent", metadata.UserAgent)
	}
	if metadata.AcceptLanguage != "" {
		request.Header.Set("Accept-Language", metadata.AcceptLanguage)
	}
	response, err := redirecter.client.Do(request)
	if err != nil {
		return nil, errors.New("request Alipay redirect")
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	location, err := response.Location()
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode >= http.StatusBadRequest || err != nil || !isTrustedPayRedirect(location) {
		return nil, errors.New("Alipay did not return a trusted redirect")
	}
	return location, nil
}

func isTrustedPayRedirect(location *url.URL) bool {
	if location == nil || location.Scheme != "https" || location.User != nil || location.Port() != "" {
		return false
	}
	host := strings.ToLower(location.Hostname())
	return strings.HasSuffix(host, ".alipay.com") || strings.HasSuffix(host, ".alipaydev.com")
}
