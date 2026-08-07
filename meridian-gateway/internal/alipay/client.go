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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

type Client struct {
	AppID           string
	PrivateKey      *rsa.PrivateKey
	AlipayPublicKey *rsa.PublicKey
	GatewayURL      string
	NotifyURL       string
	HTTPClient      *http.Client
}

type PaymentResult struct {
	OutTradeNo   string
	TradeNo      string
	OutRequestNo string
	QRContent    string
	TradeStatus  string
	RefundAmount int64
	Raw          map[string]json.RawMessage
}

type BusinessError struct {
	Code       string
	Message    string
	SubCode    string
	SubMessage string
}

func (e *BusinessError) Error() string {
	return fmt.Sprintf("alipay business error %s %s %s %s", e.Code, e.Message, e.SubCode, e.SubMessage)
}

func IsBusinessError(err error) bool {
	var target *BusinessError
	return errors.As(err, &target)
}

func IsTradeNotExist(err error) bool {
	var target *BusinessError
	return errors.As(err, &target) && target.SubCode == "ACQ.TRADE_NOT_EXIST"
}

func IsRefundNotExist(err error) bool {
	var target *BusinessError
	if !errors.As(err, &target) {
		return false
	}
	return target.SubCode == "ACQ.TRADE_NOT_EXIST" || target.SubCode == "ACQ.REFUND_NOT_EXIST"
}

func IsUncertainBusinessError(err error) bool {
	var target *BusinessError
	if !errors.As(err, &target) {
		return false
	}
	return target.Code == "20000" || strings.Contains(target.SubCode, "SYSTEM_ERROR")
}

type WebhookEvent struct {
	EventKey    string
	OutTradeNo  string
	TradeNo     string
	TradeStatus string
	TotalAmount int64
	Raw         string
}

func NewClient(credentials domain.AlipayCredentials, gatewayURL, notifyURL string) (*Client, error) {
	privateKey, err := parsePrivateKey(credentials.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse alipay private key: %w", err)
	}
	publicKey, err := parsePublicKey(credentials.AlipayPublicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse alipay public key: %w", err)
	}
	if gatewayURL == "" {
		gatewayURL = ProductionGatewayURL
	}
	return &Client{AppID: credentials.AppID, PrivateKey: privateKey, AlipayPublicKey: publicKey, GatewayURL: gatewayURL, NotifyURL: notifyURL, HTTPClient: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (c *Client) Precreate(ctx context.Context, orderNo string, amountMinor int64, subject string, timeout string) (PaymentResult, error) {
	biz := map[string]string{"out_trade_no": orderNo, "total_amount": formatAmount(amountMinor), "subject": subject}
	if timeout != "" {
		biz["timeout_express"] = timeout
	}
	return c.call(ctx, "alipay.trade.precreate", biz)
}

func (c *Client) Query(ctx context.Context, orderNo, tradeNo string) (PaymentResult, error) {
	biz := map[string]string{}
	if orderNo != "" {
		biz["out_trade_no"] = orderNo
	}
	if tradeNo != "" {
		biz["trade_no"] = tradeNo
	}
	return c.call(ctx, "alipay.trade.query", biz)
}

func (c *Client) Close(ctx context.Context, orderNo, tradeNo string) (PaymentResult, error) {
	biz := map[string]string{}
	if orderNo != "" {
		biz["out_trade_no"] = orderNo
	}
	if tradeNo != "" {
		biz["trade_no"] = tradeNo
	}
	return c.call(ctx, "alipay.trade.close", biz)
}

func (c *Client) Refund(ctx context.Context, orderNo, tradeNo, refundNo string, amountMinor int64, reason string) (PaymentResult, error) {
	biz := map[string]string{"refund_amount": formatAmount(amountMinor), "out_request_no": refundNo}
	if orderNo != "" {
		biz["out_trade_no"] = orderNo
	}
	if tradeNo != "" {
		biz["trade_no"] = tradeNo
	}
	if reason != "" {
		biz["refund_reason"] = reason
	}
	return c.call(ctx, "alipay.trade.refund", biz)
}

func (c *Client) QueryRefund(ctx context.Context, orderNo, tradeNo, refundNo string) (PaymentResult, error) {
	biz := map[string]string{"out_request_no": refundNo}
	if orderNo != "" {
		biz["out_trade_no"] = orderNo
	}
	if tradeNo != "" {
		biz["trade_no"] = tradeNo
	}
	return c.call(ctx, "alipay.trade.fastpay.refund.query", biz)
}

func (c *Client) BuildPayURL(method, orderNo string, amountMinor int64, subject, returnURL string) (string, error) {
	return c.BuildPayURLWithTimeout(method, orderNo, amountMinor, subject, returnURL, "")
}

func (c *Client) BuildPayURLWithTimeout(method, orderNo string, amountMinor int64, subject, returnURL, timeout string) (string, error) {
	if method != "alipay.trade.page.pay" && method != "alipay.trade.wap.pay" {
		return "", errors.New("unsupported pay method")
	}
	params := c.baseParams(method)
	params["notify_url"] = c.NotifyURL
	if returnURL != "" {
		params["return_url"] = returnURL
	}
	biz := map[string]string{"out_trade_no": orderNo, "total_amount": formatAmount(amountMinor), "subject": subject, "product_code": "FAST_INSTANT_TRADE_PAY"}
	if method == "alipay.trade.wap.pay" {
		biz["product_code"] = "QUICK_WAP_WAY"
	}
	if timeout != "" {
		biz["timeout_express"] = timeout
	}
	encoded, err := json.Marshal(biz)
	if err != nil {
		return "", err
	}
	params["biz_content"] = string(encoded)
	sign, err := c.sign(params)
	if err != nil {
		return "", err
	}
	params["sign"] = sign
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return c.GatewayURL + "?" + values.Encode(), nil
}

func (c *Client) VerifyNotification(values url.Values) (WebhookEvent, error) {
	sign := values.Get("sign")
	if sign == "" || values.Get("sign_type") != "RSA2" {
		return WebhookEvent{}, errors.New("missing or unsupported alipay notification signature")
	}
	params := map[string]string{}
	for key, all := range values {
		if key == "sign" || key == "sign_type" || len(all) == 0 {
			continue
		}
		params[key] = all[0]
	}
	if err := c.verify(params, sign); err != nil {
		return WebhookEvent{}, err
	}
	outTradeNo := values.Get("out_trade_no")
	tradeNo := values.Get("trade_no")
	status := values.Get("trade_status")
	eventKey := values.Get("notify_id")
	if eventKey == "" {
		eventKey = tradeNo + ":" + status + ":" + outTradeNo
	}
	amount, err := parseAmount(values.Get("total_amount"))
	if err != nil {
		return WebhookEvent{}, err
	}
	var raw strings.Builder
	raw.WriteString("{")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 {
			raw.WriteString(",")
		}
		b, _ := json.Marshal(values.Get(key))
		k, _ := json.Marshal(key)
		raw.Write(k)
		raw.WriteString(":")
		raw.Write(b)
	}
	raw.WriteString("}")
	return WebhookEvent{EventKey: eventKey, OutTradeNo: outTradeNo, TradeNo: tradeNo, TradeStatus: status, TotalAmount: amount, Raw: raw.String()}, nil
}

func (c *Client) call(ctx context.Context, method string, biz map[string]string) (PaymentResult, error) {
	params := c.baseParams(method)
	if c.NotifyURL != "" && method == "alipay.trade.precreate" {
		params["notify_url"] = c.NotifyURL
	}
	encoded, err := json.Marshal(biz)
	if err != nil {
		return PaymentResult{}, err
	}
	params["biz_content"] = string(encoded)
	sign, err := c.sign(params)
	if err != nil {
		return PaymentResult{}, err
	}
	params["sign"] = sign
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.GatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return PaymentResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return PaymentResult{}, fmt.Errorf("alipay request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return PaymentResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PaymentResult{}, fmt.Errorf("alipay HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return c.parseResponse(method, body)
}

func (c *Client) parseResponse(method string, body []byte) (PaymentResult, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return PaymentResult{}, fmt.Errorf("decode alipay response: %w", err)
	}
	root := strings.ReplaceAll(method, ".", "_") + "_response"
	rootRaw, ok := envelope[root]
	if !ok {
		return PaymentResult{}, fmt.Errorf("alipay response missing %s", root)
	}
	var rootMap map[string]json.RawMessage
	if err := json.Unmarshal(rootRaw, &rootMap); err != nil {
		return PaymentResult{}, err
	}
	var code string
	_ = json.Unmarshal(rootMap["code"], &code)
	var msg, subCode, subMsg string
	_ = json.Unmarshal(rootMap["msg"], &msg)
	_ = json.Unmarshal(rootMap["sub_code"], &subCode)
	_ = json.Unmarshal(rootMap["sub_msg"], &subMsg)
	signRaw := envelope["sign"]
	var sign string
	_ = json.Unmarshal(signRaw, &sign)
	// Alipay signs the response object's exact compact JSON.
	if sign == "" || c.AlipayPublicKey == nil {
		return PaymentResult{}, errors.New("alipay response has no verifiable signature")
	}
	if err := c.verifyContent(string(rootRaw), sign); err != nil {
		return PaymentResult{}, err
	}
	if code != "10000" {
		return PaymentResult{}, &BusinessError{Code: code, Message: msg, SubCode: subCode, SubMessage: subMsg}
	}
	result := PaymentResult{Raw: rootMap}
	_ = json.Unmarshal(rootMap["out_trade_no"], &result.OutTradeNo)
	_ = json.Unmarshal(rootMap["trade_no"], &result.TradeNo)
	_ = json.Unmarshal(rootMap["out_request_no"], &result.OutRequestNo)
	_ = json.Unmarshal(rootMap["qr_code"], &result.QRContent)
	_ = json.Unmarshal(rootMap["trade_status"], &result.TradeStatus)
	var refundAmount string
	_ = json.Unmarshal(rootMap["refund_amount"], &refundAmount)
	if refundAmount != "" {
		parsed, err := parseAmount(refundAmount)
		if err != nil {
			return PaymentResult{}, fmt.Errorf("decode alipay refund_amount: %w", err)
		}
		result.RefundAmount = parsed
	}
	return result, nil
}

func (c *Client) baseParams(method string) map[string]string {
	return map[string]string{"app_id": c.AppID, "method": method, "format": "JSON", "charset": "utf-8", "sign_type": "RSA2", "timestamp": time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"), "version": "1.0"}
}

func (c *Client) sign(params map[string]string) (string, error) {
	content := signingContent(params)
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.PrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func (c *Client) verify(params map[string]string, encoded string) error {
	return c.verifyContent(signingContent(params), encoded)
}

func (c *Client) verifyContent(content, encoded string) error {
	if c.AlipayPublicKey == nil {
		return errors.New("alipay public key is not configured")
	}
	digest := sha256.Sum256([]byte(content))
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return errors.New("invalid alipay signature encoding")
	}
	return rsa.VerifyPKCS1v15(c.AlipayPublicKey, crypto.SHA256, digest[:], signature)
}

func signingContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if value != "" && key != "sign" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

func parsePrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("PEM block missing")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}

func parsePublicKey(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("public key PEM block missing")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("unsupported RSA public key")
}

func formatAmount(minor int64) string { return strconv.FormatFloat(float64(minor)/100, 'f', 2, 64) }

func parseAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("missing total_amount")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) > 12 {
		return 0, errors.New("invalid amount")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 2 {
		return 0, errors.New("amount has more than two decimals")
	}
	for len(frac) < 2 {
		frac += "0"
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, errors.New("invalid amount")
	}
	cents := int64(0)
	if frac != "" {
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, errors.New("invalid amount")
		}
	}
	return whole*100 + cents, nil
}
