package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/gateway"
	"github.com/atlaspay/meridian-gateway/internal/security"
	"github.com/atlaspay/meridian-gateway/internal/storage"
	qrcode "github.com/skip2/go-qrcode"
)

func TestRealSQLiteHTTPStaticPaymentFlowAndTenantDeny(t *testing.T) {
	ctx := context.Background()
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "api.db"), enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := &gateway.Service{Store: store}
	adminToken := "adm_test_token_with_more_than_24_characters"
	handler := (&Server{Gateway: service, AdminToken: adminToken, BaseURL: "", Assets: WebAssets(), OpenAPI: OpenAPISpec}).Handler()
	host := httptest.NewServer(handler)
	defer host.Close()
	service.BaseURL = host.URL

	response := request(t, http.MethodGet, host.URL+"/admin/api/merchants", "", nil, "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing admin token status=%d", response.StatusCode)
	}
	response.Body.Close()

	merchant := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Merchant A", "fee_bps": 100}, http.StatusCreated)
	merchantID := stringField(t, merchant, "id")
	appResponse := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants/"+merchantID+"/applications", map[string]any{"name": "App A", "webhook_url": "https://merchant.example/webhooks"}, http.StatusCreated)
	apiKey := stringField(t, appResponse, "api_key")
	if stringField(t, appResponse, "webhook_secret") == "" {
		t.Fatal("webhook secret missing")
	}

	png, err := qrcode.Encode("https://example.com/static", qrcode.Medium, 128)
	if err != nil {
		t.Fatal(err)
	}
	codeResponse := adminMultipart(t, host.URL, adminToken, "/admin/api/merchants/"+merchantID+"/static-codes", map[string]string{"name": "Front desk", "store_name": "Store 1"}, "image", "alipay.png", png, http.StatusCreated)
	codeID := stringField(t, codeResponse, "id")

	paymentBody := map[string]any{"merchant_order_id": "order-http-1", "method": "static_alipay", "amount_minor": 1234, "currency": "CNY", "subject": "HTTP test", "static_code_id": codeID}
	payment := merchantJSON(t, host.URL, apiKey, http.MethodPost, "/v1/payments", paymentBody, http.StatusCreated)
	paymentID := stringField(t, payment, "id")
	checkoutURL := stringField(t, payment, "checkout_url")
	idempotent := merchantJSON(t, host.URL, apiKey, http.MethodPost, "/v1/payments", paymentBody, http.StatusOK)
	if stringField(t, idempotent, "id") != paymentID {
		t.Fatal("idempotent request returned another payment")
	}
	paymentBody["amount_minor"] = 1235
	merchantJSON(t, host.URL, apiKey, http.MethodPost, "/v1/payments", paymentBody, http.StatusConflict)

	merchantB := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Merchant B"}, http.StatusCreated)
	appB := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants/"+stringField(t, merchantB, "id")+"/applications", map[string]any{"name": "App B"}, http.StatusCreated)
	merchantJSON(t, host.URL, stringField(t, appB, "api_key"), http.MethodGet, "/v1/payments/"+paymentID, nil, http.StatusNotFound)

	adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants/"+merchantID+"/payments/"+paymentID+"/confirm", map[string]any{"actor": "operator", "note": "matched"}, http.StatusOK)
	paid := merchantJSON(t, host.URL, apiKey, http.MethodGet, "/v1/payments/"+paymentID, nil, http.StatusOK)
	if stringField(t, paid, "status") != "paid" {
		t.Fatalf("status=%v", paid["status"])
	}
	deliveries := adminJSON(t, host.URL, adminToken, http.MethodGet, "/admin/api/merchants/"+merchantID+"/deliveries", nil, http.StatusOK)
	if len(arrayField(t, deliveries, "items")) != 1 {
		t.Fatalf("deliveries=%v", deliveries)
	}
	audits := adminJSON(t, host.URL, adminToken, http.MethodGet, "/admin/api/merchants/"+merchantID+"/audits", nil, http.StatusOK)
	if len(arrayField(t, audits, "items")) != 1 {
		t.Fatalf("audits=%v", audits)
	}

	checkoutResponse, err := http.Get(checkoutURL)
	if err != nil {
		t.Fatal(err)
	}
	defer checkoutResponse.Body.Close()
	content, _ := io.ReadAll(checkoutResponse.Body)
	if checkoutResponse.StatusCode != http.StatusOK || !strings.Contains(string(content), "商户已确认到账。") || strings.Contains(string(content), "等待商户操作员核对到账。") || strings.Contains(string(content), "/static-code") {
		t.Fatalf("checkout response invalid: %d %s", checkoutResponse.StatusCode, content)
	}
	if checkoutResponse.Header.Get("Content-Security-Policy") == "" || checkoutResponse.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
	staticCodeResponse, err := http.Get(checkoutURL + "/static-code")
	if err != nil {
		t.Fatal(err)
	}
	staticCodeResponse.Body.Close()
	if staticCodeResponse.StatusCode != http.StatusGone {
		t.Fatalf("paid static code status=%d want %d", staticCodeResponse.StatusCode, http.StatusGone)
	}
	resultResponse, err := http.Get(host.URL + "/result?payment_id=" + url.QueryEscape(paymentID))
	if err != nil {
		t.Fatal(err)
	}
	defer resultResponse.Body.Close()
	resultContent, _ := io.ReadAll(resultResponse.Body)
	if resultResponse.StatusCode != http.StatusOK || !strings.Contains(string(resultContent), `class="pill paid"`) || !strings.Contains(string(resultContent), "支付结果") {
		t.Fatalf("result page response invalid: %d %s", resultResponse.StatusCode, resultContent)
	}
}

func TestMerchantAdminAccessAndRecoveryOperations(t *testing.T) {
	ctx := context.Background()
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "merchant-admin.db"), enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := &gateway.Service{Store: store}
	systemToken := "adm_test_system_token_more_than_24_characters"
	host := httptest.NewServer((&Server{Gateway: service, AdminToken: systemToken, Assets: WebAssets(), OpenAPI: OpenAPISpec}).Handler())
	defer host.Close()
	service.BaseURL = host.URL

	merchantA := adminJSON(t, host.URL, systemToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Merchant A"}, http.StatusCreated)
	merchantAID := stringField(t, merchantA, "id")
	merchantB := adminJSON(t, host.URL, systemToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Merchant B"}, http.StatusCreated)
	merchantBID := stringField(t, merchantB, "id")
	appResponse := adminJSON(t, host.URL, systemToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/applications", map[string]any{"name": "Merchant app"}, http.StatusCreated)
	app := appResponse["application"].(map[string]any)
	appID := stringField(t, app, "id")
	oldAPIKey := stringField(t, appResponse, "api_key")
	accessResponse := adminJSON(t, host.URL, systemToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/access-token", map[string]any{}, http.StatusOK)
	merchantToken := stringField(t, accessResponse, "admin_token")

	session := adminJSON(t, host.URL, merchantToken, http.MethodGet, "/admin/api/session", nil, http.StatusOK)
	if stringField(t, session, "role") != "merchant_admin" || stringField(t, session, "merchant_id") != merchantAID {
		t.Fatalf("merchant session scope invalid: %v", session)
	}
	merchants := adminJSON(t, host.URL, merchantToken, http.MethodGet, "/admin/api/merchants", nil, http.StatusOK)
	if items := arrayField(t, merchants, "items"); len(items) != 1 || stringField(t, items[0].(map[string]any), "id") != merchantAID {
		t.Fatalf("merchant list leaked another tenant: %v", merchants)
	}
	adminJSON(t, host.URL, merchantToken, http.MethodGet, "/admin/api/merchants/"+merchantBID+"/applications", nil, http.StatusForbidden)
	adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Forbidden"}, http.StatusForbidden)
	adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/access-token", map[string]any{}, http.StatusForbidden)

	png, err := qrcode.Encode("https://example.com/static", qrcode.Medium, 128)
	if err != nil {
		t.Fatal(err)
	}
	codeResponse := adminMultipart(t, host.URL, merchantToken, "/admin/api/merchants/"+merchantAID+"/static-codes", map[string]string{"name": "Counter"}, "image", "code.png", png, http.StatusCreated)
	codeID := stringField(t, codeResponse, "id")
	payment := adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/payments", map[string]any{"application_id": appID, "merchant_order_id": "admin-created", "method": "static_alipay", "amount_minor": 100, "currency": "CNY", "subject": "Admin created", "static_code_id": codeID}, http.StatusCreated)
	paymentID := stringField(t, payment, "id")
	adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/payments/"+paymentID+"/confirm", map[string]any{"note": "matched"}, http.StatusOK)
	if targets, err := store.ClaimDueDeliveries(ctx, 10); err != nil || len(targets) != 0 {
		t.Fatalf("empty webhook delivery was not skipped: targets=%d err=%v", len(targets), err)
	}
	deliveries := adminJSON(t, host.URL, merchantToken, http.MethodGet, "/admin/api/merchants/"+merchantAID+"/deliveries", nil, http.StatusOK)
	delivery := arrayField(t, deliveries, "items")[0].(map[string]any)
	deliveryID := stringField(t, delivery, "id")
	adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/deliveries/"+deliveryID+"/retry", map[string]any{}, http.StatusBadRequest)
	adminJSON(t, host.URL, merchantToken, http.MethodPatch, "/admin/api/merchants/"+merchantAID+"/applications/"+appID, map[string]any{"name": "Merchant app", "webhook_url": "https://merchant.example/webhooks", "status": "active"}, http.StatusOK)
	adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/deliveries/"+deliveryID+"/retry", map[string]any{}, http.StatusOK)

	rotated := adminJSON(t, host.URL, merchantToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/applications/"+appID+"/credentials", map[string]any{}, http.StatusOK)
	newAPIKey := stringField(t, rotated, "api_key")
	merchantJSON(t, host.URL, oldAPIKey, http.MethodGet, "/v1/payments/"+paymentID, nil, http.StatusUnauthorized)
	merchantJSON(t, host.URL, newAPIKey, http.MethodGet, "/v1/payments/"+paymentID, nil, http.StatusOK)

	newAccess := adminJSON(t, host.URL, systemToken, http.MethodPost, "/admin/api/merchants/"+merchantAID+"/access-token", map[string]any{}, http.StatusOK)
	adminJSON(t, host.URL, merchantToken, http.MethodGet, "/admin/api/session", nil, http.StatusUnauthorized)
	adminJSON(t, host.URL, stringField(t, newAccess, "admin_token"), http.MethodGet, "/admin/api/session", nil, http.StatusOK)
}

func TestAdminLocalOnlyRejectsRemoteAddressBeforeServiceAccess(t *testing.T) {
	server := &Server{AdminToken: "adm_test_token_with_more_than_24_characters", AdminLocalOnly: true, Assets: WebAssets(), OpenAPI: OpenAPISpec}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/admin/api/merchants", nil)
	req.RemoteAddr = "203.0.113.10:4242"
	req.Header.Set("Authorization", "Bearer "+server.AdminToken)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403 body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminLocalOnlyRejectsForwardedLoopbackRequests(t *testing.T) {
	const token = "adm_test_token_with_more_than_24_characters"
	server := &Server{AdminToken: token, Environment: "development", AdminLocalOnly: true, Assets: WebAssets(), OpenAPI: OpenAPISpec}

	for _, test := range []struct {
		name   string
		header string
		value  string
	}{
		{name: "standard forwarded", header: "Forwarded", value: "for=203.0.113.10;proto=https"},
		{name: "forwarded for", header: "X-Forwarded-For", value: "203.0.113.10"},
		{name: "real ip", header: "X-Real-IP", value: "203.0.113.10"},
		{name: "cloudflare connecting ip", header: "CF-Connecting-IP", value: "203.0.113.10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, path := range []string{"/admin", "/admin/api/merchants"} {
				req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
				req.RemoteAddr = "127.0.0.1:4242"
				req.Header.Set(test.header, test.value)
				req.Header.Set("Authorization", "Bearer "+token)
				recorder := httptest.NewRecorder()
				server.Handler().ServeHTTP(recorder, req)
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("path=%s status=%d want 403 body=%s", path, recorder.Code, recorder.Body.String())
				}
				if strings.Contains(recorder.Body.String(), token) {
					t.Fatalf("path=%s exposed admin token", path)
				}
			}
		})
	}
}

func TestRealSQLiteHTTPAlipaySandboxConfiguration(t *testing.T) {
	redirectRequests := make(chan alipay.PayForm, 8)
	gatewayLocations := make(chan string, 8)
	redirecter := payRedirecterFunc(func(_ context.Context, form alipay.PayForm, _ alipay.PayRedirectMetadata) (*url.URL, error) {
		redirectRequests <- form
		return url.Parse(<-gatewayLocations)
	})
	ctx := context.Background()
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "alipay-api.db"), enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := &gateway.Service{Store: store}
	adminToken := "adm_alipay_test_token_more_than_24_chars"
	host := httptest.NewServer((&Server{Gateway: service, AdminToken: adminToken, Assets: WebAssets(), OpenAPI: OpenAPISpec, AlipayRedirecter: redirecter}).Handler())
	defer host.Close()
	service.BaseURL = host.URL

	merchant := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants", map[string]any{"name": "Sandbox Merchant"}, http.StatusCreated)
	merchantID := stringField(t, merchant, "id")
	appResponse := adminJSON(t, host.URL, adminToken, http.MethodPost, "/admin/api/merchants/"+merchantID+"/applications", map[string]any{"name": "Sandbox App"}, http.StatusCreated)
	app, ok := appResponse["application"].(map[string]any)
	if !ok {
		t.Fatalf("application missing in %v", appResponse)
	}
	appID := stringField(t, app, "id")
	apiKey := stringField(t, appResponse, "api_key")
	privateKeyPEM, publicKeyPEM := alipayCredentialPEMs(t)
	path := "/admin/api/merchants/" + merchantID + "/alipay"
	body := map[string]any{
		"environment":           "sandbox",
		"app_id":                "sandbox-app-id",
		"private_key_pem":       privateKeyPEM,
		"alipay_public_key_pem": publicKeyPEM,
	}
	configured := adminJSON(t, host.URL, adminToken, http.MethodPut, path, body, http.StatusOK)
	if configured["environment"] != "sandbox" || configured["notify_url"] != host.URL+"/v1/webhooks/alipay" {
		t.Fatalf("unexpected Alipay response: %v", configured)
	}
	for _, key := range []string{"gateway_url", "private_key_pem", "alipay_public_key_pem", "has_certificate_chain"} {
		if _, found := configured[key]; found {
			t.Fatalf("response exposes removed or sensitive field %s: %v", key, configured)
		}
	}
	account, credentials, err := store.GetAlipayAccount(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	if account.GatewayURL != alipay.SandboxGatewayURL || account.NotifyURL != host.URL+"/v1/webhooks/alipay" || credentials.AppID != "sandbox-app-id" {
		t.Fatalf("persisted Alipay account mismatch: account=%+v credentials=%+v", account, credentials)
	}

	client, err := alipay.NewClient(credentials, account.GatewayURL, account.NotifyURL)
	if err != nil {
		t.Fatal(err)
	}
	signedURL, err := client.BuildPayURL("alipay.trade.page.pay", "pay-redirect", 1, "支付宝网页测试", host.URL+"/result")
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchantID, ApplicationID: appID, MerchantOrderID: "page-order-1", Method: "alipay_page", AmountMinor: 1, Currency: "CNY", Subject: "支付宝网页测试", ReturnURL: host.URL + "/result", ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchantID, created.Payment.ID, "", "WAIT_BUYER_PAY", "", signedURL); err != nil {
		t.Fatal(err)
	}
	view := merchantJSON(t, host.URL, apiKey, http.MethodGet, "/v1/payments/"+created.Payment.ID, nil, http.StatusOK)
	redirectURL := stringField(t, view, "redirect_url")
	wantRedirectURL := host.URL + "/checkout/" + created.Payment.CheckoutToken + "/redirect"
	if redirectURL != wantRedirectURL || strings.Contains(redirectURL, "alipaydev.com") {
		t.Fatalf("redirect_url=%q want %q", redirectURL, wantRedirectURL)
	}
	checkoutResponse, err := http.Get(host.URL + "/checkout/" + created.Payment.CheckoutToken)
	if err != nil {
		t.Fatal(err)
	}
	checkoutBody, _ := io.ReadAll(checkoutResponse.Body)
	checkoutResponse.Body.Close()
	if checkoutResponse.StatusCode != http.StatusOK || !strings.Contains(string(checkoutBody), `checkout-body-redirect`) || !strings.Contains(string(checkoutBody), `method="post"`) || !strings.Contains(string(checkoutBody), `action="/checkout/`+created.Payment.CheckoutToken+`/redirect"`) || !strings.Contains(string(checkoutBody), `name="sign"`) || strings.Contains(string(checkoutBody), `name="charset"`) || strings.Contains(string(checkoutBody), `href="/checkout/`+created.Payment.CheckoutToken+`/redirect"`) || strings.Contains(string(checkoutBody), `支付宝跳转支付`) {
		t.Fatalf("checkout redirect invalid: %d %s", checkoutResponse.StatusCode, checkoutBody)
	}
	if !strings.Contains(checkoutResponse.Header.Get("Content-Security-Policy"), "https://openapi-sandbox.dl.alipaydev.com") {
		t.Fatalf("checkout CSP does not allow sandbox form action: %s", checkoutResponse.Header.Get("Content-Security-Policy"))
	}
	for _, allowedRedirectDomain := range []string{"https://*.alipay.com", "https://*.alipaydev.com"} {
		if !strings.Contains(checkoutResponse.Header.Get("Content-Security-Policy"), allowedRedirectDomain) {
			t.Fatalf("checkout CSP does not allow Alipay redirect domain %s: %s", allowedRedirectDomain, checkoutResponse.Header.Get("Content-Security-Policy"))
		}
	}
	redirectResponse, err := http.Get(wantRedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	redirectBody, _ := io.ReadAll(redirectResponse.Body)
	redirectResponse.Body.Close()
	if redirectResponse.StatusCode != http.StatusOK || !strings.Contains(string(redirectBody), `method="post"`) || !strings.Contains(string(redirectBody), `action="/checkout/`+created.Payment.CheckoutToken+`/redirect"`) || !strings.Contains(string(redirectBody), `name="sign"`) || strings.Contains(string(redirectBody), `name="charset"`) {
		t.Fatalf("Alipay POST bridge invalid: %d %s", redirectResponse.StatusCode, redirectBody)
	}
	if !strings.Contains(redirectResponse.Header.Get("Content-Security-Policy"), "https://openapi-sandbox.dl.alipaydev.com") {
		t.Fatalf("CSP does not allow sandbox form action: %s", redirectResponse.Header.Get("Content-Security-Policy"))
	}
	expectedForm, err := alipay.ParsePayURL(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	formValues := url.Values{}
	for _, field := range expectedForm.Fields {
		formValues.Add(field.Name, field.Value)
	}
	noRedirectClient := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	expectedNextURL := "https://unitradeprod-sandbox.dl.alipaydev.com/appAssign.htm?session=test"
	gatewayLocations <- expectedNextURL
	postResponse, err := noRedirectClient.PostForm(wantRedirectURL, formValues)
	if err != nil {
		t.Fatal(err)
	}
	postResponse.Body.Close()
	if postResponse.StatusCode != http.StatusSeeOther || postResponse.Header.Get("Location") != expectedNextURL {
		t.Fatalf("Alipay redirect POST status=%d location=%q", postResponse.StatusCode, postResponse.Header.Get("Location"))
	}
	captured := <-redirectRequests
	capturedValues := url.Values{}
	for _, field := range captured.Fields {
		capturedValues.Add(field.Name, field.Value)
	}
	if captured.Action != expectedForm.Action || capturedValues.Encode() != formValues.Encode() {
		t.Fatalf("unexpected Alipay redirect request: action=%s values_match=%t", captured.Action, capturedValues.Encode() == formValues.Encode())
	}

	copyFormValues := func() url.Values {
		copy := url.Values{}
		for key, values := range formValues {
			copy[key] = append([]string(nil), values...)
		}
		return copy
	}
	invalidForms := map[string]url.Values{
		"missing":   copyFormValues(),
		"tampered":  copyFormValues(),
		"duplicate": copyFormValues(),
		"extra":     copyFormValues(),
	}
	invalidForms["missing"].Del("sign")
	invalidForms["tampered"].Set("sign", "tampered")
	invalidForms["duplicate"].Add("sign", formValues.Get("sign"))
	invalidForms["extra"].Set("unexpected", "value")
	for name, values := range invalidForms {
		t.Run("reject redirect "+name, func(t *testing.T) {
			response, err := noRedirectClient.PostForm(wantRedirectURL, values)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest || response.Header.Get("Location") != "" {
				t.Fatalf("status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
			}
		})
	}
	if len(redirectRequests) != 0 {
		t.Fatal("invalid form reached the Alipay redirect adapter")
	}
	missingResponse, err := http.Get(host.URL + "/checkout/not-a-token/redirect")
	if err != nil {
		t.Fatal(err)
	}
	missingResponse.Body.Close()
	if missingResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("missing redirect status=%d", missingResponse.StatusCode)
	}
	missingPostResponse, err := noRedirectClient.PostForm(host.URL+"/checkout/not-a-token/redirect", formValues)
	if err != nil {
		t.Fatal(err)
	}
	missingPostResponse.Body.Close()
	if missingPostResponse.StatusCode != http.StatusNotFound || missingPostResponse.Header.Get("Location") != "" {
		t.Fatalf("missing redirect POST status=%d location=%q", missingPostResponse.StatusCode, missingPostResponse.Header.Get("Location"))
	}
	if _, err := store.ClosePayment(ctx, merchantID, created.Payment.ID, "test"); err != nil {
		t.Fatal(err)
	}
	closedCheckout, err := http.Get(host.URL + "/checkout/" + created.Payment.CheckoutToken)
	if err != nil {
		t.Fatal(err)
	}
	closedBody, _ := io.ReadAll(closedCheckout.Body)
	closedCheckout.Body.Close()
	if closedCheckout.StatusCode != http.StatusOK || strings.Contains(string(closedBody), `data-payment-action`) || strings.Contains(string(closedBody), `name="sign"`) {
		t.Fatalf("closed checkout remains actionable: status=%d body=%s", closedCheckout.StatusCode, closedBody)
	}
	closedRedirect, err := http.Get(wantRedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	closedRedirect.Body.Close()
	if closedRedirect.StatusCode != http.StatusConflict {
		t.Fatalf("closed redirect GET status=%d", closedRedirect.StatusCode)
	}
	closedPost, err := noRedirectClient.PostForm(wantRedirectURL, formValues)
	if err != nil {
		t.Fatal(err)
	}
	closedPost.Body.Close()
	if closedPost.StatusCode != http.StatusConflict || len(redirectRequests) != 0 {
		t.Fatalf("closed redirect POST status=%d adapter_requests=%d", closedPost.StatusCode, len(redirectRequests))
	}

	body["environment"] = "custom"
	adminJSON(t, host.URL, adminToken, http.MethodPut, path, body, http.StatusBadRequest)
	after := adminJSON(t, host.URL, adminToken, http.MethodGet, path, nil, http.StatusOK)
	if after["environment"] != "sandbox" {
		t.Fatalf("invalid environment mutated account: %v", after)
	}
}

type payRedirecterFunc func(context.Context, alipay.PayForm, alipay.PayRedirectMetadata) (*url.URL, error)

func (function payRedirecterFunc) ResolvePayRedirect(ctx context.Context, form alipay.PayForm, metadata alipay.PayRedirectMetadata) (*url.URL, error) {
	return function(ctx, form, metadata)
}

func TestCheckoutAssetsRefreshAndRemoveNonPayableActions(t *testing.T) {
	script, err := fs.ReadFile(WebAssets(), "checkout.js")
	if err != nil {
		t.Fatal(err)
	}
	template, err := fs.ReadFile(WebAssets(), "checkout.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`data.payable`, `[data-payment-action]`, `pageshow`, `visibilitychange`, `created","pending`} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("checkout script missing %s", required)
		}
	}
	if !strings.Contains(string(template), `{{if $.payable}}`) || !strings.Contains(string(template), `data-payment-action`) {
		t.Fatal("checkout template does not gate payment actions")
	}
}

func TestAdminAlipayFormUsesEnvironmentWithoutCustomProtocolFields(t *testing.T) {
	content, err := fs.ReadFile(WebAssets(), "admin.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	for _, required := range []string{
		`name="environment"`, `value="sandbox"`, `value="production"`, `name="app_id"`, `name="private_key_pem"`, `name="alipay_public_key_pem"`,
		`https://open.alipay.com/develop/sandbox/app`, `https://opendocs.alipay.com/common/02kkv7`, `https://open.alipay.com/develop/manage`,
		`https://opendocs.alipay.com/common/02kipk`, `https://opendocs.alipay.com/common/02kdnc`, `https://b.alipay.com/page/product-mall/all-product`,
		`支付宝 RSA2 公钥`, `它不是应用公钥`, `MERIDIAN_BASE_URL`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("admin Alipay form missing %s", required)
		}
	}
	for _, removed := range []string{`name="gateway_url"`, `name="notify_url"`, `app_cert_pem`, `alipay_public_cert_pem`, `alipay_root_cert_pem`} {
		if strings.Contains(script, removed) {
			t.Fatalf("admin Alipay form still exposes %s", removed)
		}
	}
}

func TestAdminMerchantExperienceContracts(t *testing.T) {
	htmlContent, err := fs.ReadFile(WebAssets(), "admin.html")
	if err != nil {
		t.Fatal(err)
	}
	scriptContent, err := fs.ReadFile(WebAssets(), "admin.js")
	if err != nil {
		t.Fatal(err)
	}
	styleContent, err := fs.ReadFile(WebAssets(), "styles.css")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlContent)
	script := string(scriptContent)
	styles := string(styleContent)
	for _, required := range []string{`管理令牌`, `id="rotate-merchant-token"`, `aria-label="订单状态"`} {
		if !strings.Contains(html, required) {
			t.Fatalf("admin page missing merchant experience contract %s", required)
		}
	}
	for _, required := range []string{`/admin/api/session`, `name="application_id"`, `/credentials`, `/retry`, `statusLabels`, `filteredPayments()`} {
		if !strings.Contains(script, required) {
			t.Fatalf("admin script missing merchant experience contract %s", required)
		}
	}
	if strings.Contains(script, `name="api_key"`) {
		t.Fatal("admin payment form still asks for an application API key")
	}
	for _, required := range []string{`overflow-x: auto`, `min-width: 760px`, `table-layout: auto`} {
		if !strings.Contains(styles, required) {
			t.Fatalf("admin styles missing responsive table contract %s", required)
		}
	}
	if strings.Contains(styles, `table-layout: fixed`) {
		t.Fatal("admin tables still use the narrow-screen fixed layout")
	}
}

func TestAdminPagePrefillsTokenOnlyInDevelopment(t *testing.T) {
	const token = "adm_test_token_with_more_than_24_characters"
	const baseURL = "https://pay.example.test"
	for _, test := range []struct {
		name        string
		environment string
		wantToken   bool
	}{
		{name: "development", environment: "development", wantToken: true},
		{name: "production", environment: "production", wantToken: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{AdminToken: token, BaseURL: baseURL, Environment: test.environment, Assets: WebAssets(), OpenAPI: OpenAPISpec}
			req := httptest.NewRequest(http.MethodGet, "http://example.test/admin", nil)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			containsToken := strings.Contains(recorder.Body.String(), token)
			if containsToken != test.wantToken {
				t.Fatalf("token presence=%v want %v body=%s", containsToken, test.wantToken, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `data-base-url="`+baseURL+`"`) {
				t.Fatalf("admin page missing configured base URL")
			}
		})
	}
}

func adminJSON(t *testing.T, host, token, method, path string, body any, want int) map[string]any {
	t.Helper()
	return doJSON(t, host, token, method, path, body, want)
}
func merchantJSON(t *testing.T, host, token, method, path string, body any, want int) map[string]any {
	t.Helper()
	return doJSON(t, host, token, method, path, body, want)
}
func doJSON(t *testing.T, host, token, method, path string, body any, want int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	response := request(t, method, host+path, token, reader, "application/json")
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if len(content) > 0 {
		if err := json.Unmarshal(content, &result); err != nil {
			t.Fatalf("decode response %d: %v body=%s", response.StatusCode, err, content)
		}
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, want, content)
	}
	return result
}
func request(t *testing.T, method, urlValue, token string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, urlValue, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func adminMultipart(t *testing.T, host, token, path string, fields map[string]string, fileField, fileName string, file []byte, want int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		_ = writer.WriteField(key, value)
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	response := request(t, http.MethodPost, host+path, token, &body, writer.FormDataContentType())
	defer response.Body.Close()
	content, _ := io.ReadAll(response.Body)
	if response.StatusCode != want {
		t.Fatalf("multipart status=%d want=%d body=%s", response.StatusCode, want, content)
	}
	var result map[string]any
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func stringField(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	got, ok := value[key].(string)
	if !ok || got == "" {
		t.Fatalf("field %s missing in %v", key, value)
	}
	return got
}
func arrayField(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	got, ok := value[key].([]any)
	if !ok {
		t.Fatalf("array %s missing in %v", key, value)
	}
	return got
}

func alipayCredentialPEMs(t *testing.T) (string, string) {
	t.Helper()
	appPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	alipayPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&alipayPrivate.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(appPrivate)})
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return string(privateKeyPEM), string(publicKeyPEM)
}
