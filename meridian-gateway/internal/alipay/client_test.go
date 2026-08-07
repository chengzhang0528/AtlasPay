package alipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

func TestPrecreateSignsRequestAndVerifiesResponse(t *testing.T) {
	appPrivate := newRSAKey(t)
	alipayPrivate := newRSAKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded;charset=utf-8" {
			t.Errorf("Content-Type=%q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			w.WriteHeader(400)
			return
		}
		params := map[string]string{}
		for key, values := range r.PostForm {
			if key != "sign" && len(values) > 0 {
				params[key] = values[0]
			}
		}
		if err := verifyWithKey(&appPrivate.PublicKey, signingContent(params), r.PostForm.Get("sign")); err != nil {
			t.Errorf("request signature: %v", err)
		}
		if got := r.PostForm.Get("method"); got != "alipay.trade.precreate" {
			t.Errorf("method=%s", got)
		}
		var biz map[string]string
		if err := json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz); err != nil {
			t.Errorf("decode biz_content: %v", err)
		} else if biz["timeout_express"] != "45m" {
			t.Errorf("timeout_express=%q want 45m", biz["timeout_express"])
		} else if biz["subject"] != "支付宝测试订单" {
			t.Errorf("subject=%q", biz["subject"])
		}
		root := `{"code":"10000","msg":"Success","out_trade_no":"pay_1","trade_no":"20260001","qr_code":"https://qr.example/1"}`
		sign := signWithKey(t, alipayPrivate, root)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"alipay_trade_precreate_response":%s,"sign":%q}`, root, sign)
	}))
	defer server.Close()
	client, err := NewClient(domain.AlipayCredentials{AppID: "app-1", PrivateKeyPEM: privatePEM(appPrivate), AlipayPublicKeyPEM: publicPEM(&alipayPrivate.PublicKey)}, server.URL, "https://merchant.example/alipay")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Precreate(context.Background(), "pay_1", 1234, "支付宝测试订单", "45m")
	if err != nil {
		t.Fatal(err)
	}
	if result.OutTradeNo != "pay_1" || result.TradeNo != "20260001" || result.QRContent == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	redirect, err := client.BuildPayURLWithTimeout("alipay.trade.page.pay", "pay_1", 1234, "Test order", "https://merchant.example/result", "2h")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	var redirectBiz map[string]string
	if err := json.Unmarshal([]byte(parsed.Query().Get("biz_content")), &redirectBiz); err != nil {
		t.Fatal(err)
	}
	if redirectBiz["timeout_express"] != "2h" {
		t.Fatalf("redirect timeout_express=%q want 2h", redirectBiz["timeout_express"])
	}
}

func TestNotificationVerificationRejectsTampering(t *testing.T) {
	appPrivate := newRSAKey(t)
	alipayPrivate := newRSAKey(t)
	client, err := NewClient(domain.AlipayCredentials{AppID: "app-1", PrivateKeyPEM: privatePEM(appPrivate), AlipayPublicKeyPEM: publicPEM(&alipayPrivate.PublicKey)}, "https://openapi.example/gateway.do", "")
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"notify_id": {"notify-1"}, "out_trade_no": {"pay_1"}, "trade_no": {"trade_1"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"12.34"}, "sign_type": {"RSA2"}}
	params := map[string]string{}
	for key, all := range values {
		if key != "sign" && key != "sign_type" {
			params[key] = all[0]
		}
	}
	values.Set("sign", signWithKey(t, alipayPrivate, signingContent(params)))
	event, err := client.VerifyNotification(values)
	if err != nil {
		t.Fatal(err)
	}
	if event.TotalAmount != 1234 || event.EventKey != "notify-1" {
		t.Fatalf("unexpected event: %+v", event)
	}
	values.Set("total_amount", "99.99")
	if _, err := client.VerifyNotification(values); err == nil {
		t.Fatal("tampered notification must fail")
	}
}

func TestRefundQuerySignsRequestAndClassifiesBusinessErrors(t *testing.T) {
	appPrivate := newRSAKey(t)
	alipayPrivate := newRSAKey(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("method") != "alipay.trade.fastpay.refund.query" {
			t.Fatalf("method=%s", r.PostForm.Get("method"))
		}
		var biz map[string]string
		if err := json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz); err != nil {
			t.Fatal(err)
		}
		if biz["out_trade_no"] != "pay_1" || biz["trade_no"] != "trade_1" || biz["out_request_no"] != "refund_1" {
			t.Fatalf("unexpected refund query: %v", biz)
		}
		root := `{"code":"10000","msg":"Success","out_trade_no":"pay_1","trade_no":"trade_1","out_request_no":"refund_1","refund_amount":"12.34"}`
		if requests == 2 {
			root = `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"trade not exist"}`
		}
		sign := signWithKey(t, alipayPrivate, root)
		fmt.Fprintf(w, `{"alipay_trade_fastpay_refund_query_response":%s,"sign":%q}`, root, sign)
	}))
	defer server.Close()
	client, err := NewClient(domain.AlipayCredentials{AppID: "app-1", PrivateKeyPEM: privatePEM(appPrivate), AlipayPublicKeyPEM: publicPEM(&alipayPrivate.PublicKey)}, server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.QueryRefund(context.Background(), "pay_1", "trade_1", "refund_1")
	if err != nil {
		t.Fatal(err)
	}
	if result.OutRequestNo != "refund_1" || result.RefundAmount != 1234 {
		t.Fatalf("unexpected refund query result: %+v", result)
	}
	_, err = client.QueryRefund(context.Background(), "pay_1", "trade_1", "refund_1")
	if !IsBusinessError(err) || !IsTradeNotExist(err) || !IsRefundNotExist(err) {
		t.Fatalf("error classification failed: %v", err)
	}
	if !IsUncertainBusinessError(&BusinessError{Code: "20000"}) || !IsUncertainBusinessError(&BusinessError{SubCode: "ACQ.SYSTEM_ERROR"}) || IsUncertainBusinessError(err) {
		t.Fatal("uncertain business error classification failed")
	}
}

func TestParseAmount(t *testing.T) {
	for input, want := range map[string]int64{"0.01": 1, "12": 1200, "12.3": 1230, "999.99": 99999} {
		got, err := parseAmount(input)
		if err != nil || got != want {
			t.Fatalf("parseAmount(%q)=(%d,%v), want %d", input, got, err, want)
		}
	}
	if _, err := parseAmount("1.234"); err == nil {
		t.Fatal("expected precision error")
	}
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func privatePEM(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}
func publicPEM(key *rsa.PublicKey) string {
	bytes, _ := x509.MarshalPKIXPublicKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: bytes}))
}
func signWithKey(t *testing.T, key *rsa.PrivateKey, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}
func verifyWithKey(key *rsa.PublicKey, content, encoded string) error {
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], signature)
}
