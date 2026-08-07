package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/security"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

func TestWorkerDeliversSignedWebhookAndPersistsOutcome(t *testing.T) {
	ctx := context.Background()
	received := make(chan *http.Request, 1)
	bodyReceived := make(chan []byte, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- r.Clone(context.Background())
		bodyReceived <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	enc, _ := security.NewEncryptor(bytes.Repeat([]byte{9}, 32))
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "worker.db"), enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, err := store.CreateApplication(ctx, merchant.ID, "App", target.URL)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := store.CreateStaticCode(ctx, merchant.ID, "Code", "", "CNY", "image/png", []byte("png"))
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "order-worker", Method: domain.MethodStaticAlipay, AmountMinor: 100, Currency: "CNY", Subject: "Worker", StaticCodeID: code.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmPayment(ctx, merchant.ID, created.Payment.ID, "operator", ""); err != nil {
		t.Fatal(err)
	}
	worker := &Worker{Store: store, HTTPClient: &http.Client{Timeout: 2 * time.Second}}
	if err := worker.deliver(ctx); err != nil {
		t.Fatal(err)
	}
	request := <-received
	body := <-bodyReceived
	if request.Header.Get("X-Meridian-Event") != "payment.paid" {
		t.Fatalf("event=%s", request.Header.Get("X-Meridian-Event"))
	}
	timestampUnix, err := strconv.ParseInt(request.Header.Get("X-Meridian-Timestamp"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	want := "v1=" + security.WebhookSignature(app.WebhookSecret, time.Unix(timestampUnix, 0).UTC(), body)
	if got := request.Header.Get("X-Meridian-Signature"); got != want {
		t.Fatalf("signature=%s want=%s", got, want)
	}
	if !strings.Contains(string(body), created.Payment.ID) {
		t.Fatalf("payload does not identify payment: %s", body)
	}
	deliveries, err := store.ListDeliveries(ctx, merchant.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "delivered" || deliveries[0].Attempts != 1 || deliveries[0].LastStatusCode != http.StatusNoContent {
		t.Fatalf("unexpected delivery: %+v", deliveries)
	}
}

func TestExpiryWorkerPersistsBackoffAfterTransientChannelFailure(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	expires := time.Now().UTC().Add(-time.Minute)
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "worker-expired", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Worker expired", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-worker", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	client := &stubAlipayClient{queryErr: context.DeadlineExceeded}
	worker := &Worker{Store: store, Service: serviceWithAlipay(store, client)}
	if err := worker.closeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetPayment(ctx, merchant.ID, created.Payment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.StatusPending || after.LastError == "" || client.queryCalls != 1 {
		t.Fatalf("expiry retry was not scheduled: payment=%+v query_calls=%d", after, client.queryCalls)
	}
	if err := worker.closeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	if client.queryCalls != 1 {
		t.Fatalf("expiry worker ignored persisted backoff: query_calls=%d", client.queryCalls)
	}
}

func TestRefundWorkerReconcilesDueUnknownResult(t *testing.T) {
	ctx := context.Background()
	store := newGatewayTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	created, err := store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "worker-refund", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Worker refund"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-worker-refund", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-worker-refund", "TRADE_SUCCESS", "worker-refund-paid", "{}")
	if err != nil {
		t.Fatal(err)
	}
	begin, err := store.BeginRefund(ctx, storage.RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "worker-refund-1", AmountMinor: 100}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRefundUnknown(ctx, merchant.ID, begin.Refund.ID, "response timeout"); err != nil {
		t.Fatal(err)
	}
	client := &stubAlipayClient{refundQueryResult: alipay.PaymentResult{TradeNo: "trade-worker-refund", OutRequestNo: "worker-refund-1", RefundAmount: 100}}
	worker := &Worker{Store: store, Service: serviceWithAlipay(store, client)}
	time.Sleep(1100 * time.Millisecond)
	if err := worker.reconcileRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	refund, payment, err := store.GetRefundAndPayment(ctx, merchant.ID, begin.Refund.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refund.Status != "succeeded" || payment.Status != domain.StatusRefunded || client.refundQueryCalls != 1 || client.refundCalls != 0 {
		t.Fatalf("refund worker did not reconcile query result: refund=%+v payment=%+v query_calls=%d refund_calls=%d", refund, payment, client.refundQueryCalls, client.refundCalls)
	}
}
