package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/security"
)

func TestLegacyDatabaseMigratesMerchantAdminToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); CREATE TABLE merchants (id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', fee_bps INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL); INSERT INTO schema_migrations(version,applied_at) VALUES(1,'2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path, enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var columnCount, versionCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('merchants') WHERE name='admin_token_hash'`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=2`).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 1 || versionCount != 1 {
		t.Fatalf("migration column=%d version=%d", columnCount, versionCount)
	}
}

func TestLegacyDatabaseMigratesPaymentRecoveryColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-payment-recovery.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := string(schema)
	for _, line := range []string{
		"    close_attempts INTEGER NOT NULL DEFAULT 0,\n",
		"    close_next_attempt_at TEXT,\n",
	} {
		legacySchema = strings.Replace(legacySchema, line, "", 1)
	}
	legacySchema = strings.Replace(legacySchema,
		"    channel_refund_id TEXT NOT NULL DEFAULT '',\n    last_error TEXT NOT NULL DEFAULT '',\n    attempts INTEGER NOT NULL DEFAULT 0,\n    next_attempt_at TEXT,\n",
		"    channel_refund_id TEXT NOT NULL DEFAULT '',\n    last_error TEXT NOT NULL DEFAULT '',\n", 1)
	for _, raw := range strings.Split(legacySchema, ";") {
		if statement := strings.TrimSpace(raw); statement != "" {
			if _, err := db.Exec(statement); err != nil {
				db.Close()
				t.Fatalf("create legacy schema: %v", err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(1,'2026-08-01T00:00:00Z'),(2,'2026-08-02T00:00:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path, enc)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`UPDATE payments SET close_attempts=0,close_next_attempt_at=NULL; UPDATE refunds SET attempts=0,next_attempt_at=NULL`); err != nil {
		t.Fatalf("payment recovery columns were not migrated: %v", err)
	}
	var migrationCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=3`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("payment recovery migration missing: count=%d err=%v", migrationCount, err)
	}
}

func TestMerchantAndApplicationCredentialRotationStoresOnlyProtectedValues(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, err := store.CreateMerchant(ctx, "Credential merchant", 0)
	if err != nil {
		t.Fatal(err)
	}
	firstMerchantToken, err := store.RotateMerchantAdminToken(ctx, merchant.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMerchantByAdminToken(ctx, firstMerchantToken); err != nil {
		t.Fatal(err)
	}
	secondMerchantToken, err := store.RotateMerchantAdminToken(ctx, merchant.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMerchantByAdminToken(ctx, firstMerchantToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old merchant token still authenticates: %v", err)
	}
	if _, err := store.GetMerchantByAdminToken(ctx, secondMerchantToken); err != nil {
		t.Fatal(err)
	}
	var merchantTokenHash string
	if err := store.db.QueryRowContext(ctx, `SELECT admin_token_hash FROM merchants WHERE id=?`, merchant.ID).Scan(&merchantTokenHash); err != nil {
		t.Fatal(err)
	}
	if merchantTokenHash != security.Digest(secondMerchantToken) || strings.Contains(merchantTokenHash, secondMerchantToken) {
		t.Fatal("merchant token was not stored as the expected digest")
	}

	app, oldAPIKey, err := store.CreateApplication(ctx, merchant.ID, "Original app", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateApplication(ctx, merchant.ID, app.ID, "Updated app", "https://merchant.example/webhooks", "active", "test")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated app" || updated.WebhookURL == "" {
		t.Fatalf("application update missing: %+v", updated)
	}
	rotated, newAPIKey, err := store.RotateApplicationCredentials(ctx, merchant.ID, app.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.WebhookSecret == "" || newAPIKey == "" {
		t.Fatal("rotated application credentials missing")
	}
	if _, err := store.GetApplicationByAPIKey(ctx, oldAPIKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old API key still authenticates: %v", err)
	}
	if _, err := store.GetApplicationByAPIKey(ctx, newAPIKey); err != nil {
		t.Fatal(err)
	}
	var apiHash, secretCipher string
	if err := store.db.QueryRowContext(ctx, `SELECT api_key_hash,webhook_secret_cipher FROM applications WHERE id=?`, app.ID).Scan(&apiHash, &secretCipher); err != nil {
		t.Fatal(err)
	}
	if apiHash != security.Digest(newAPIKey) || strings.Contains(secretCipher, rotated.WebhookSecret) {
		t.Fatal("application credentials were not protected at rest")
	}
}

func TestSkippedDeliveryCanRetryOnlyWithinMerchant(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Delivery merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "Delivery app", "")
	code, _ := store.CreateStaticCode(ctx, merchant.ID, "Counter", "", "CNY", "image/png", []byte("png"))
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "delivery-retry", Method: domain.MethodStaticAlipay, AmountMinor: 100, Currency: "CNY", Subject: "Retry", StaticCodeID: code.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmPayment(ctx, merchant.ID, created.Payment.ID, "test", "paid"); err != nil {
		t.Fatal(err)
	}
	if targets, err := store.ClaimDueDeliveries(ctx, 10); err != nil || len(targets) != 0 {
		t.Fatalf("empty webhook should skip delivery: targets=%d err=%v", len(targets), err)
	}
	deliveries, err := store.ListDeliveries(ctx, merchant.ID, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != "skipped" {
		t.Fatalf("delivery was not skipped: %+v err=%v", deliveries, err)
	}
	other, _ := store.CreateMerchant(ctx, "Other merchant", 0)
	if err := store.RetryDelivery(ctx, other.ID, deliveries[0].ID, "test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant retry must be hidden: %v", err)
	}
	if err := store.RetryDelivery(ctx, merchant.ID, deliveries[0].ID, "test"); err != nil {
		t.Fatal(err)
	}
	retried, err := store.GetDelivery(ctx, merchant.ID, deliveries[0].ID)
	if err != nil || retried.Status != "pending" || retried.Attempts != 0 || retried.LastError != "" {
		t.Fatalf("delivery retry state invalid: %+v err=%v", retried, err)
	}
}

func TestOpenConfiguresSQLiteForConcurrentRuntimeAccess(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()

	var foreignKeys, busyTimeout int
	var journalMode string
	if err := store.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
		t.Fatalf("sqlite pragmas foreign_keys=%d busy_timeout=%d journal_mode=%s", foreignKeys, busyTimeout, journalMode)
	}

	if _, err := store.CreateMerchant(ctx, "Reader merchant", 0); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id FROM merchants`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected a merchant row")
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		t.Fatal(err)
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := store.CreateMerchant(ctx, "Concurrent writer", 0)
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("write failed while a read cursor was active: %v", err)
		}
	case <-time.After(2 * time.Second):
		_ = rows.Close()
		t.Fatalf("write blocked while a read cursor was active: %v", <-writeDone)
	}
}

func TestAlipayCredentialsAreEncryptedAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, err := store.CreateMerchant(ctx, "Encrypted Alipay", 0)
	if err != nil {
		t.Fatal(err)
	}
	credentials := domain.AlipayCredentials{
		AppID:              "sandbox-app-id",
		PrivateKeyPEM:      "private-key-material",
		AlipayPublicKeyPEM: "alipay-public-key-material",
	}
	_, err = store.PutAlipayAccount(ctx, merchant.ID, "https://openapi-sandbox.dl.alipaydev.com/gateway.do", "https://merchant.example/v1/webhooks/alipay", credentials)
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext string
	if err := store.db.QueryRowContext(ctx, `SELECT credentials_cipher FROM channel_accounts WHERE merchant_id=?`, merchant.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, credentials.PrivateKeyPEM) || strings.Contains(ciphertext, credentials.AlipayPublicKeyPEM) {
		t.Fatal("Alipay credential material is visible in stored ciphertext")
	}
	account, got, err := store.GetAlipayAccount(ctx, merchant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != credentials || !account.HasPrivateKey || !account.HasAlipayPublicKey {
		t.Fatalf("credential round trip mismatch: account=%+v credentials=%+v", account, got)
	}
}

func TestStaticPaymentIdempotencyTenantIsolationAndAtomicConfirmation(t *testing.T) {
	ctx := context.Background()
	store, path, enc := newTestStore(t)
	defer store.Close()
	merchant, err := store.CreateMerchant(ctx, "Merchant A", 125)
	if err != nil {
		t.Fatal(err)
	}
	app, _, err := store.CreateApplication(ctx, merchant.ID, "App A", "https://merchant.example/webhook")
	if err != nil {
		t.Fatal(err)
	}
	code, err := store.CreateStaticCode(ctx, merchant.ID, "Front desk", "Store 1", "CNY", "image/png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	input := CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "order-1", Method: domain.MethodStaticAlipay, AmountMinor: 10000, Currency: "CNY", Subject: "Order 1", StaticCodeID: code.ID}
	first, err := store.CreatePayment(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Fatal("first payment must be created")
	}
	second, err := store.CreatePayment(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.Payment.ID != first.Payment.ID {
		t.Fatalf("idempotency failed: %+v", second)
	}
	conflict := input
	conflict.AmountMinor = 10001
	if _, err := store.CreatePayment(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	other, err := store.CreateMerchant(ctx, "Merchant B", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPayment(ctx, other.ID, first.Payment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read must be not found, got %v", err)
	}

	paid, err := store.ConfirmPayment(ctx, merchant.ID, first.Payment.ID, "operator-1", "bank statement matched")
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != domain.StatusPaid {
		t.Fatalf("status=%s", paid.Status)
	}
	if _, err := store.ConfirmPayment(ctx, merchant.ID, first.Payment.ID, "operator-1", "duplicate"); err != nil {
		t.Fatal(err)
	}
	assertCount(t, store, "fees", 1)
	assertCount(t, store, "audit_logs", 1)
	assertCount(t, store, "outbox_deliveries", 1)
	var fee int64
	if err := store.db.QueryRowContext(ctx, `SELECT amount_minor FROM fees WHERE payment_id=?`, paid.ID).Scan(&fee); err != nil {
		t.Fatal(err)
	}
	if fee != 125 {
		t.Fatalf("fee=%d want 125", fee)
	}
	if _, err := store.ClosePayment(ctx, merchant.ID, paid.ID, "operator-1"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("paid payment close should fail, got %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, enc)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.GetPayment(ctx, merchant.ID, paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != domain.StatusPaid || persisted.PaidAt == nil {
		t.Fatalf("payment did not persist: %+v", persisted)
	}
}

func TestRefundCannotExceedPaidAmountAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "dynamic-1", Method: domain.MethodAlipayQR, AmountMinor: 1000, Currency: "CNY", Subject: "Dynamic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-1", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-1", "TRADE_SUCCESS", "event-1", "{}")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, applied, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-1", "TRADE_SUCCESS", "event-1", "{}")
	if err != nil || applied || duplicate.ID != paid.ID {
		t.Fatalf("duplicate channel event was not idempotent: applied=%v payment=%+v err=%v", applied, duplicate, err)
	}
	assertCount(t, store, "fees", 1)
	assertCount(t, store, "channel_events", 1)
	assertCount(t, store, "outbox_deliveries", 1)
	begin, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "refund-1", AmountMinor: 400}, "test")
	if err != nil {
		t.Fatal(err)
	}
	refund, payment := begin.Refund, begin.Payment
	if payment.Status != domain.StatusRefunding {
		t.Fatalf("status=%s", payment.Status)
	}
	pendingRetry, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "refund-1", AmountMinor: 400}, "test")
	if err != nil || pendingRetry.Created || pendingRetry.Refund.ID != refund.ID {
		t.Fatalf("pending refund retry was not idempotent: result=%+v err=%v", pendingRetry, err)
	}
	completed, payment, err := store.CompleteRefund(ctx, merchant.ID, refund.ID, "refund-channel-1")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" || payment.Status != domain.StatusPartiallyRefunded || payment.RefundedMinor != 400 {
		t.Fatalf("unexpected refund: %+v %+v", completed, payment)
	}
	unchanged, err := store.FailRefund(ctx, merchant.ID, refund.ID, "late failure")
	if err != nil || unchanged.Status != "succeeded" {
		t.Fatalf("late failure changed completed refund: refund=%+v err=%v", unchanged, err)
	}
	paymentAfterLateFailure, err := store.GetPayment(ctx, merchant.ID, paid.ID)
	if err != nil || paymentAfterLateFailure.Status != domain.StatusPartiallyRefunded || paymentAfterLateFailure.RefundedMinor != 400 {
		t.Fatalf("late failure rolled payment back: payment=%+v err=%v", paymentAfterLateFailure, err)
	}
	idempotent, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "refund-1", AmountMinor: 400}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Created || idempotent.Refund.ID != refund.ID {
		t.Fatal("refund idempotency returned another record")
	}
	if _, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "refund-2", AmountMinor: 700}, "test"); err == nil {
		t.Fatal("over-refund must fail")
	}
}

func TestLatePaidEventDoesNotReopenClosedPayment(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "late-event", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Late event"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-late", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	closed, err := store.ClosePayment(ctx, merchant.ID, created.Payment.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	after, applied, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "notify", "trade-late", "TRADE_SUCCESS", "late-1", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if applied || after.Status != domain.StatusClosed || closed.ID != after.ID {
		t.Fatalf("late event changed terminal state: applied=%v payment=%+v", applied, after)
	}
	assertCount(t, store, "fees", 0)
}

func TestRefundRequiresPaymentApplicationOwnership(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	appA, _, _ := store.CreateApplication(ctx, merchant.ID, "App A", "")
	appB, _, _ := store.CreateApplication(ctx, merchant.ID, "App B", "")
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: appA.ID, MerchantOrderID: "owned-refund", Method: domain.MethodAlipayQR, AmountMinor: 1000, Currency: "CNY", Subject: "Owned refund"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-owned", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-owned", "TRADE_SUCCESS", "owned-event", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: appB.ID, PaymentID: paid.ID, MerchantRefund: "cross-app-refund", AmountMinor: 100}, "api:"+appB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-application refund must be hidden, got %v", err)
	}
	assertCount(t, store, "refunds", 0)
}

func TestVerifiedPaidEventSettlesLocallyExpiredPayment(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	expires := time.Now().UTC().Add(-time.Minute)
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "expired-payment", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Expired", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-expired", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	after, applied, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "notify", "trade-expired", "TRADE_SUCCESS", "expired-event", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if !applied || after.Status != domain.StatusPaid || after.ChannelOrderID != "trade-expired" {
		t.Fatalf("verified paid event was not applied: applied=%v payment=%+v", applied, after)
	}
	assertCount(t, store, "fees", 1)
}

func TestPaymentAndRefundRetriesUsePersistedDueTimes(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newTestStore(t)
	defer store.Close()
	merchant, _ := store.CreateMerchant(ctx, "Merchant", 0)
	app, _, _ := store.CreateApplication(ctx, merchant.ID, "App", "")
	expires := time.Now().UTC().Add(-time.Minute)
	created, err := store.CreatePayment(ctx, CreatePaymentInput{MerchantID: merchant.ID, ApplicationID: app.ID, MerchantOrderID: "retry-payment", Method: domain.MethodAlipayQR, AmountMinor: 100, Currency: "CNY", Subject: "Retry", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdatePaymentChannel(ctx, merchant.ID, created.Payment.ID, "trade-retry", "WAIT_BUYER_PAY", "qr", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SchedulePaymentCloseRetry(ctx, merchant.ID, created.Payment.ID, "temporary close error"); err != nil {
		t.Fatal(err)
	}
	if due, err := store.ListExpiredPayments(ctx, 10); err != nil || len(due) != 0 {
		t.Fatalf("close retry became due immediately: due=%+v err=%v", due, err)
	}
	var closeAttempts, closeAttemptRows int
	var closeNext string
	if err := store.db.QueryRowContext(ctx, `SELECT close_attempts,close_next_attempt_at FROM payments WHERE id=?`, created.Payment.ID).Scan(&closeAttempts, &closeNext); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_attempts WHERE payment_id=? AND result='close_failed'`, created.Payment.ID).Scan(&closeAttemptRows); err != nil {
		t.Fatal(err)
	}
	if closeAttempts != 1 || closeAttemptRows != 1 || closeNext == "" {
		t.Fatalf("close retry was not persisted: attempts=%d rows=%d next=%q", closeAttempts, closeAttemptRows, closeNext)
	}
	past := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `UPDATE payments SET close_next_attempt_at=? WHERE id=?`, past, created.Payment.ID); err != nil {
		t.Fatal(err)
	}
	if due, err := store.ListExpiredPayments(ctx, 10); err != nil || len(due) != 1 || due[0].PaymentID != created.Payment.ID {
		t.Fatalf("persisted close retry did not become due: due=%+v err=%v", due, err)
	}

	paid, _, err := store.ApplyChannelPaid(ctx, merchant.ID, created.Payment.ID, "test", "trade-retry", "TRADE_SUCCESS", "retry-paid", "{}")
	if err != nil {
		t.Fatal(err)
	}
	begin, err := store.BeginRefund(ctx, RefundInput{MerchantID: merchant.ID, ApplicationID: app.ID, PaymentID: paid.ID, MerchantRefund: "refund-retry", AmountMinor: 100}, "test")
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := store.MarkRefundUnknown(ctx, merchant.ID, begin.Refund.ID, "refund response timeout")
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Attempts != 1 || unknown.NextAttemptAt == nil {
		t.Fatalf("refund retry was not persisted: %+v", unknown)
	}
	if due, err := store.ListDueRefunds(ctx, 10); err != nil || len(due) != 0 {
		t.Fatalf("refund retry became due immediately: due=%+v err=%v", due, err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE refunds SET next_attempt_at=? WHERE id=?`, past, begin.Refund.ID); err != nil {
		t.Fatal(err)
	}
	if due, err := store.ListDueRefunds(ctx, 10); err != nil || len(due) != 1 || due[0].RefundID != begin.Refund.ID {
		t.Fatalf("persisted refund retry did not become due: due=%+v err=%v", due, err)
	}
	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=3`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("payment recovery migration missing: count=%d err=%v", migrationCount, err)
	}
}

func newTestStore(t *testing.T) (*Store, string, *security.Encryptor) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meridian.db")
	enc, err := security.NewEncryptor(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path, enc)
	if err != nil {
		t.Fatal(err)
	}
	return store, path, enc
}
func assertCount(t *testing.T, store *Store, table string, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d want %d", table, got, want)
	}
}
