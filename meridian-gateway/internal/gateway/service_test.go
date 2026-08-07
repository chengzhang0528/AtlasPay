package gateway

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/security"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

type stubAlipayClient struct {
	queryResult       alipay.PaymentResult
	queryErr          error
	closeErr          error
	refundResult      alipay.PaymentResult
	refundErr         error
	refundQueryResult alipay.PaymentResult
	refundQueryErr    error
	queryCalls        int
	closeCalls        int
	refundCalls       int
	refundQueryCalls  int
}

func (c *stubAlipayClient) Precreate(context.Context, string, int64, string, string) (alipay.PaymentResult, error) {
	return alipay.PaymentResult{}, nil
}
func (c *stubAlipayClient) Query(context.Context, string, string) (alipay.PaymentResult, error) {
	c.queryCalls++
	return c.queryResult, c.queryErr
}
func (c *stubAlipayClient) Close(context.Context, string, string) (alipay.PaymentResult, error) {
	c.closeCalls++
	return alipay.PaymentResult{}, c.closeErr
}
func (c *stubAlipayClient) Refund(context.Context, string, string, string, int64, string) (alipay.PaymentResult, error) {
	c.refundCalls++
	return c.refundResult, c.refundErr
}
func (c *stubAlipayClient) QueryRefund(context.Context, string, string, string) (alipay.PaymentResult, error) {
	c.refundQueryCalls++
	return c.refundQueryResult, c.refundQueryErr
}
func (c *stubAlipayClient) BuildPayURLWithTimeout(string, string, int64, string, string, string) (string, error) {
	return "", nil
}
func (c *stubAlipayClient) VerifyNotification(url.Values) (alipay.WebhookEvent, error) {
	return alipay.WebhookEvent{}, nil
}

func serviceWithAlipay(store *storage.Store, client AlipayClient) *Service {
	return &Service{Store: store, AlipayClientFactory: func(context.Context, string) (AlipayClient, domain.AlipayAccount, error) {
		return client, domain.AlipayAccount{AppID: "app-test"}, nil
	}}
}

func TestRefundRejectsPaymentFromAnotherApplication(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	appA, _, _ := store.CreateApplication(ctx, merchant.ID, "App A", "")
	appB, _, _ := store.CreateApplication(ctx, merchant.ID, "App B", "")
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: appA.ID, MerchantOrderID: "service-owned-refund", Method: domain.MethodAlipayQR, AmountMinor: 1000, Currency: "CNY", Subject: "Owned refund"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-service", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-service", "TRADE_SUCCESS", "service-owned-event", "{}")
	if err != nil {
		t.Fatal(err)
	}

	service := &Service{Store: store}
	if _, err := service.Refund(ctx, appB, paid.ID, "cross-app", 100, "test"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("cross-application refund must be rejected as not found, got %v", err)
	}
	refunds, err := store.ListRefunds(ctx, merchant.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(refunds) != 0 {
		t.Fatalf("cross-application refund mutated storage: refunds=%d", len(refunds))
	}
}

func TestClosePaymentRejectsPaidBeforeProviderLookup(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "service-paid-close", Method: domain.MethodAlipayQR, AmountMinor: 1000, Currency: "CNY", Subject: "Paid close"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-paid", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-paid", "TRADE_SUCCESS", "service-paid-event", "{}")
	if err != nil {
		t.Fatal(err)
	}

	service := &Service{Store: store}
	if _, err := service.ClosePayment(ctx, app, paid.ID, "api:test"); !errors.Is(err, storage.ErrInvalidState) {
		t.Fatalf("paid payment close should fail before provider lookup, got %v", err)
	}
}

func TestCloseExpiredPaymentReconcilesPaidBeforeClosing(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	expires := time.Now().UTC().Add(-time.Minute)
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "expired-paid", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Expired paid", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	client := &stubAlipayClient{queryResult: alipay.PaymentResult{TradeNo: "trade-expired-paid", TradeStatus: "TRADE_SUCCESS"}}
	service := serviceWithAlipay(store, client)
	if err := service.CloseExpiredPayment(ctx, merchant.ID, created.Payment.ID); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetPayment(ctx, merchant.ID, created.Payment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.StatusPaid || after.ChannelOrderID != "trade-expired-paid" || client.closeCalls != 0 {
		t.Fatalf("expired paid reconciliation failed: payment=%+v close_calls=%d", after, client.closeCalls)
	}
}

func TestCloseExpiredPaymentTreatsMissingProviderTradeAsClosed(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	expires := time.Now().UTC().Add(-time.Minute)
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "expired-missing", Method: domain.MethodAlipayPage, AmountMinor: 100, Currency: "CNY", Subject: "Expired missing", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	client := &stubAlipayClient{queryErr: &alipay.BusinessError{Code: "40004", SubCode: "ACQ.TRADE_NOT_EXIST"}}
	service := serviceWithAlipay(store, client)
	if err := service.CloseExpiredPayment(ctx, merchant.ID, created.Payment.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetPayment(ctx, merchant.ID, created.Payment.ID)
	if after.Status != domain.StatusClosed || client.closeCalls != 0 {
		t.Fatalf("missing provider trade was not closed locally: payment=%+v close_calls=%d", after, client.closeCalls)
	}
}

func TestRefundTimeoutRemainsRecoverableAndFullRefundRetryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "refund-timeout", Method: domain.MethodAlipayQR, AmountMinor: 1000, Currency: "CNY", Subject: "Refund timeout"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-refund", "TRADE_SUCCESS", "refund-paid", "{}")
	if err != nil {
		t.Fatal(err)
	}
	client := &stubAlipayClient{refundErr: context.DeadlineExceeded}
	service := serviceWithAlipay(store, client)
	pending, err := service.Refund(ctx, app, paid.ID, "refund-recover", 1000, "test")
	if err != nil || pending.Status != "pending" {
		t.Fatalf("refund timeout was not accepted as pending: refund=%+v err=%v", pending, err)
	}
	refunds, _ := store.ListRefunds(ctx, merchant.ID, 10)
	afterTimeout, _ := store.GetPayment(ctx, merchant.ID, paid.ID)
	if len(refunds) != 1 || refunds[0].Status != "pending" || refunds[0].Attempts != 1 || refunds[0].NextAttemptAt == nil || afterTimeout.Status != domain.StatusRefunding {
		t.Fatalf("ambiguous refund was not retained: refunds=%+v payment=%+v", refunds, afterTimeout)
	}
	client.refundErr = nil
	client.refundQueryResult = alipay.PaymentResult{TradeNo: "trade-refund", OutRequestNo: "refund-recover", RefundAmount: 1000}
	recovered, err := service.Refund(ctx, app, paid.ID, "refund-recover", 1000, "test")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "succeeded" || client.refundCalls != 1 || client.refundQueryCalls != 1 {
		t.Fatalf("refund recovery failed: refund=%+v refund_calls=%d query_calls=%d", recovered, client.refundCalls, client.refundQueryCalls)
	}
	idempotent, err := service.Refund(ctx, app, paid.ID, "refund-recover", 1000, "test")
	if err != nil || idempotent.ID != recovered.ID || client.refundCalls != 1 || client.refundQueryCalls != 1 {
		t.Fatalf("full refund retry was not idempotent: refund=%+v err=%v", idempotent, err)
	}
}

func TestPaymentReturnURLDefaultsToReadOnlyMeridianResult(t *testing.T) {
	service := &Service{BaseURL: "https://pay.example.test/"}
	payment := &domain.Payment{ID: "pay_result"}
	if got := service.paymentReturnURL(payment); got != "https://pay.example.test/result?payment_id=pay_result" {
		t.Fatalf("default return URL=%q", got)
	}
	payment.ReturnURL = "https://merchant.example/result"
	if got := service.paymentReturnURL(payment); got != payment.ReturnURL {
		t.Fatalf("merchant return URL was replaced: %q", got)
	}
}

func newGatewayTestStore(t *testing.T) *storage.Store {
	t.Helper()
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "gateway.db"), enc)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
