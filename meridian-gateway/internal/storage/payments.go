package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/security"
)

type CreatePaymentInput struct {
	MerchantID      string
	ApplicationID   string
	MerchantOrderID string
	Method          domain.PaymentMethod
	AmountMinor     int64
	Currency        string
	Subject         string
	StaticCodeID    string
	ReturnURL       string
	ExpiresAt       *time.Time
}

type CreatePaymentResult struct {
	Payment domain.Payment
	Created bool
}

const paymentSelect = `SELECT id,merchant_id,application_id,merchant_order_id,method,amount_minor,currency,subject,status,COALESCE(static_code_id,''),checkout_token,channel_order_id,channel_status,qr_content,redirect_url,return_url,expires_at,paid_at,closed_at,refunded_minor,last_error,created_at,updated_at FROM payments`
const refundSelect = `SELECT id,merchant_id,payment_id,merchant_refund_id,amount_minor,status,channel_refund_id,last_error,attempts,next_attempt_at,created_at,updated_at FROM refunds`

type scanner interface{ Scan(...any) error }

func scanPayment(s scanner) (domain.Payment, error) {
	var item domain.Payment
	var method, status string
	var expiresAt, paidAt, closedAt, createdAt, updatedAt sql.NullString
	if err := s.Scan(&item.ID, &item.MerchantID, &item.ApplicationID, &item.MerchantOrderID, &method, &item.AmountMinor, &item.Currency, &item.Subject, &status, &item.StaticCodeID, &item.CheckoutToken, &item.ChannelOrderID, &item.ChannelStatus, &item.QRContent, &item.RedirectURL, &item.ReturnURL, &expiresAt, &paidAt, &closedAt, &item.RefundedMinor, &item.LastError, &createdAt, &updatedAt); err != nil {
		return domain.Payment{}, err
	}
	item.Method = domain.PaymentMethod(method)
	item.Status = domain.PaymentStatus(status)
	item.ExpiresAt = parseOptionalTime(expiresAt)
	item.PaidAt = parseOptionalTime(paidAt)
	item.ClosedAt = parseOptionalTime(closedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanRefund(s scanner) (domain.Refund, error) {
	var item domain.Refund
	var nextAttemptAt, createdAt, updatedAt sql.NullString
	if err := s.Scan(&item.ID, &item.MerchantID, &item.PaymentID, &item.MerchantRefund, &item.AmountMinor, &item.Status, &item.ChannelRefund, &item.LastError, &item.Attempts, &nextAttemptAt, &createdAt, &updatedAt); err != nil {
		return domain.Refund{}, err
	}
	item.NextAttemptAt = parseOptionalTime(nextAttemptAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func (s *Store) CreatePayment(ctx context.Context, input CreatePaymentInput) (CreatePaymentResult, error) {
	if err := domain.ValidatePaymentInput(input.MerchantOrderID, input.Method, input.AmountMinor, input.Currency, input.Subject); err != nil {
		return CreatePaymentResult{}, err
	}
	app, err := s.getApplicationWithSecret(ctx, input.ApplicationID)
	if err != nil {
		return CreatePaymentResult{}, err
	}
	if app.MerchantID != input.MerchantID || app.Status != "active" {
		return CreatePaymentResult{}, ErrNotFound
	}
	if input.Method == domain.MethodStaticAlipay {
		if input.StaticCodeID == "" {
			return CreatePaymentResult{}, errors.New("static_code_id is required for static_alipay")
		}
		code, _, err := s.GetStaticCode(ctx, input.MerchantID, input.StaticCodeID)
		if err != nil {
			return CreatePaymentResult{}, err
		}
		if !code.Enabled || code.Currency != input.Currency {
			return CreatePaymentResult{}, errors.New("static code is disabled or currency does not match")
		}
	} else if input.StaticCodeID != "" {
		return CreatePaymentResult{}, errors.New("static_code_id is only valid for static_alipay")
	}
	if input.ExpiresAt == nil {
		expires := time.Now().UTC().Add(30 * time.Minute)
		input.ExpiresAt = &expires
	}
	if input.ReturnURL != "" && len(input.ReturnURL) > 2048 {
		return CreatePaymentResult{}, errors.New("return_url is too long")
	}
	id, err := security.NewID("pay")
	if err != nil {
		return CreatePaymentResult{}, err
	}
	checkoutToken, err := security.NewSecret("checkout")
	if err != nil {
		return CreatePaymentResult{}, err
	}
	created := now()
	status := domain.StatusCreated
	if input.Method == domain.MethodStaticAlipay {
		status = domain.StatusPending
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO payments(id,merchant_id,application_id,merchant_order_id,method,amount_minor,currency,subject,status,static_code_id,checkout_token,return_url,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, input.MerchantID, input.ApplicationID, input.MerchantOrderID, input.Method, input.AmountMinor, input.Currency, input.Subject, status, emptyToNil(input.StaticCodeID), checkoutToken, input.ReturnURL, nullableTime(input.ExpiresAt), created, created)
	if err != nil {
		if isUniqueError(err) {
			existing, getErr := s.getPaymentByAppOrder(ctx, input.ApplicationID, input.MerchantOrderID)
			if getErr != nil {
				return CreatePaymentResult{}, getErr
			}
			if existing.AmountMinor != input.AmountMinor || existing.Currency != input.Currency || existing.Subject != input.Subject || existing.Method != input.Method || existing.StaticCodeID != input.StaticCodeID || existing.ReturnURL != input.ReturnURL {
				return CreatePaymentResult{}, ErrConflict
			}
			return CreatePaymentResult{Payment: existing, Created: false}, nil
		}
		return CreatePaymentResult{}, fmt.Errorf("create payment: %w", err)
	}
	item, err := s.GetPayment(ctx, input.MerchantID, id)
	if err != nil {
		return CreatePaymentResult{}, err
	}
	return CreatePaymentResult{Payment: item, Created: true}, nil
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) getPaymentByAppOrder(ctx context.Context, applicationID, merchantOrderID string) (domain.Payment, error) {
	return scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE application_id=? AND merchant_order_id=?`, applicationID, merchantOrderID))
}

func (s *Store) GetPayment(ctx context.Context, merchantID, id string) (domain.Payment, error) {
	item, err := scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND id=?`, merchantID, id))
	if errorsIsNoRows(err) {
		return domain.Payment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetPaymentByCheckoutToken(ctx context.Context, token string) (domain.Payment, error) {
	item, err := scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE checkout_token=?`, token))
	if errorsIsNoRows(err) {
		return domain.Payment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetPaymentByID(ctx context.Context, id string) (domain.Payment, error) {
	item, err := scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE id=?`, id))
	if errorsIsNoRows(err) {
		return domain.Payment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) SetPaymentLastError(ctx context.Context, merchantID, id, reason string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE payments SET last_error=?,updated_at=? WHERE merchant_id=? AND id=?`, reason, now(), merchantID, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePaymentChannel(ctx context.Context, merchantID, id, channelOrderID, channelStatus, qrContent, redirectURL string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE payments SET status=CASE WHEN status='created' THEN 'pending' ELSE status END,channel_order_id=?,channel_status=?,qr_content=?,redirect_url=?,last_error='',updated_at=? WHERE merchant_id=? AND id=? AND status IN ('created','pending')`, channelOrderID, channelStatus, qrContent, redirectURL, now(), merchantID, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		_, err := s.GetPayment(ctx, merchantID, id)
		if err != nil {
			return err
		}
		return ErrInvalidState
	}
	return nil
}

func (s *Store) FailPayment(ctx context.Context, merchantID, id, reason string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE payments SET status='failed',last_error=?,updated_at=? WHERE merchant_id=? AND id=? AND status IN ('created','pending')`, reason, now(), merchantID, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return s.ensurePaymentState(ctx, merchantID, id, domain.StatusFailed)
	}
	return nil
}

func (s *Store) ensurePaymentState(ctx context.Context, merchantID, id string, status domain.PaymentStatus) error {
	item, err := s.GetPayment(ctx, merchantID, id)
	if err != nil {
		return err
	}
	if item.Status == status {
		return nil
	}
	return ErrInvalidState
}

func (s *Store) ConfirmPayment(ctx context.Context, merchantID, id, actor, note string) (domain.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Payment{}, err
	}
	defer tx.Rollback()
	item, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND id=?`, merchantID, id))
	if errorsIsNoRows(err) {
		return domain.Payment{}, ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	if item.Status == domain.StatusPaid || item.Status == domain.StatusPartiallyRefunded || item.Status == domain.StatusRefunded {
		return item, tx.Commit()
	}
	if item.ExpiresAt != nil && !item.ExpiresAt.After(time.Now().UTC()) {
		return domain.Payment{}, errors.New("payment has expired")
	}
	if item.Method != domain.MethodStaticAlipay {
		return domain.Payment{}, errors.New("manual confirmation is only available for static_alipay")
	}
	if !domain.CanTransition(item.Status, domain.StatusPaid) {
		return domain.Payment{}, ErrInvalidState
	}
	if err := s.settlePaidTx(ctx, tx, &item, actor, note, "manual_confirmation"); err != nil {
		return domain.Payment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Payment{}, err
	}
	return item, nil
}

func (s *Store) ApplyChannelPaid(ctx context.Context, merchantID, id, actor, channelOrderID, channelStatus, eventKey, rawPayload string) (domain.Payment, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Payment{}, false, err
	}
	defer tx.Rollback()
	item, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND id=?`, merchantID, id))
	if errorsIsNoRows(err) {
		return domain.Payment{}, false, ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, false, err
	}
	if eventKey != "" {
		result, insertErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO channel_events(id,merchant_id,channel,event_key,raw_payload,processed_at) VALUES(?,?,?,?,?,?)`, mustID("evt"), merchantID, "alipay", eventKey, rawPayload, now())
		if insertErr != nil {
			return domain.Payment{}, false, insertErr
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return item, false, tx.Commit()
		}
	}
	if channelOrderID != "" && item.ChannelOrderID != "" && channelOrderID != item.ChannelOrderID {
		return domain.Payment{}, false, ErrConflict
	}
	if item.Status == domain.StatusPaid || item.Status == domain.StatusPartiallyRefunded || item.Status == domain.StatusRefunded {
		if item.ChannelOrderID == "" && channelOrderID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE payments SET channel_order_id=?,channel_status=?,updated_at=? WHERE id=?`, channelOrderID, channelStatus, now(), item.ID); err != nil {
				return domain.Payment{}, false, err
			}
			item.ChannelOrderID = channelOrderID
			item.ChannelStatus = channelStatus
		}
		return item, false, tx.Commit()
	}
	if !domain.CanTransition(item.Status, domain.StatusPaid) {
		return item, false, tx.Commit()
	}
	if channelOrderID != "" {
		item.ChannelOrderID = channelOrderID
	}
	item.ChannelStatus = channelStatus
	if err := s.settlePaidTx(ctx, tx, &item, actor, "", "channel_paid"); err != nil {
		return domain.Payment{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET channel_order_id=?,channel_status=?,updated_at=? WHERE id=?`, item.ChannelOrderID, channelStatus, now(), item.ID); err != nil {
		return domain.Payment{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Payment{}, false, err
	}
	return item, true, nil
}

func (s *Store) settlePaidTx(ctx context.Context, tx *sql.Tx, item *domain.Payment, actor, note, action string) error {
	paidAt := now()
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status='paid',paid_at=?,last_error='',updated_at=? WHERE id=?`, paidAt, paidAt, item.ID); err != nil {
		return err
	}
	item.Status = domain.StatusPaid
	item.PaidAt = parseOptionalTime(sql.NullString{String: paidAt, Valid: true})
	var feeBPS int
	if err := tx.QueryRowContext(ctx, `SELECT fee_bps FROM merchants WHERE id=?`, item.MerchantID).Scan(&feeBPS); err != nil {
		return err
	}
	fee := item.AmountMinor * int64(feeBPS) / 10000
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO fees(id,payment_id,merchant_id,kind,amount_minor,currency,created_at) VALUES(?,?,?,?,?,?,?)`, mustID("fee"), item.ID, item.MerchantID, "processing", fee, item.Currency, paidAt); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"note": note, "status": item.Status, "amount_minor": item.AmountMinor, "fee_minor": fee})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), item.MerchantID, actor, action, "payment", item.ID, details, paidAt); err != nil {
		return err
	}
	if err := s.enqueueEventTx(ctx, tx, item, "payment.paid", paidAt); err != nil {
		return err
	}
	return nil
}

func (s *Store) enqueueEventTx(ctx context.Context, tx *sql.Tx, item *domain.Payment, eventType, created string) error {
	payload, err := json.Marshal(map[string]any{
		"id": item.ID, "merchant_id": item.MerchantID, "merchant_order_id": item.MerchantOrderID,
		"method": item.Method, "amount_minor": item.AmountMinor, "currency": item.Currency,
		"status": item.Status, "channel_order_id": item.ChannelOrderID, "paid_at": item.PaidAt,
		"event_type": eventType,
	})
	if err != nil {
		return err
	}
	return s.enqueuePayloadTx(ctx, tx, item.MerchantID, item.ApplicationID, eventType, item.ID, payload, created)
}

func (s *Store) enqueuePayloadTx(ctx context.Context, tx *sql.Tx, merchantID, applicationID, eventType, aggregateID string, payload []byte, created string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO outbox_deliveries(id,merchant_id,application_id,event_type,aggregate_id,payload,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("out"), merchantID, applicationID, eventType, aggregateID, payload, created, created)
	return err
}

func (s *Store) MarkAbnormal(ctx context.Context, merchantID, id, actor, note string) error {
	if _, err := s.GetPayment(ctx, merchantID, id); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]string{"note": strings.TrimSpace(note)})
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "payment.abnormal", "payment", id, details, now())
	return err
}

func (s *Store) ClosePayment(ctx context.Context, merchantID, id, actor string) (domain.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Payment{}, err
	}
	defer tx.Rollback()
	item, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND id=?`, merchantID, id))
	if errorsIsNoRows(err) {
		return domain.Payment{}, ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, err
	}
	if item.Status == domain.StatusClosed {
		return item, tx.Commit()
	}
	if item.Status != domain.StatusPending && item.Status != domain.StatusCreated {
		return domain.Payment{}, ErrInvalidState
	}
	closed := now()
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status='closed',closed_at=?,last_error='',updated_at=? WHERE id=?`, closed, closed, item.ID); err != nil {
		return domain.Payment{}, err
	}
	item.Status = domain.StatusClosed
	item.ClosedAt = parseOptionalTime(sql.NullString{String: closed, Valid: true})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "payment.closed", "payment", id, "{}", closed); err != nil {
		return domain.Payment{}, err
	}
	if err := s.enqueueEventTx(ctx, tx, &item, "payment.closed", closed); err != nil {
		return domain.Payment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Payment{}, err
	}
	return item, nil
}

type RefundInput struct {
	MerchantID     string
	ApplicationID  string
	PaymentID      string
	MerchantRefund string
	AmountMinor    int64
}

type BeginRefundResult struct {
	Refund  domain.Refund
	Payment domain.Payment
	Created bool
}

func (s *Store) BeginRefund(ctx context.Context, input RefundInput, actor string) (BeginRefundResult, error) {
	if input.AmountMinor <= 0 || input.MerchantRefund == "" {
		return BeginRefundResult{}, errors.New("merchant_refund_id and positive amount_minor are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BeginRefundResult{}, err
	}
	defer tx.Rollback()
	item, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND application_id=? AND id=?`, input.MerchantID, input.ApplicationID, input.PaymentID))
	if errorsIsNoRows(err) {
		return BeginRefundResult{}, ErrNotFound
	}
	if err != nil {
		return BeginRefundResult{}, err
	}
	existing, existingErr := scanRefund(tx.QueryRowContext(ctx, refundSelect+` WHERE merchant_id=? AND merchant_refund_id=?`, input.MerchantID, input.MerchantRefund))
	if existingErr == nil {
		if existing.PaymentID != input.PaymentID || existing.AmountMinor != input.AmountMinor {
			return BeginRefundResult{}, ErrConflict
		}
		return BeginRefundResult{Refund: existing, Payment: item, Created: false}, tx.Commit()
	}
	if !errorsIsNoRows(existingErr) {
		return BeginRefundResult{}, existingErr
	}
	if item.Method == domain.MethodStaticAlipay {
		return BeginRefundResult{}, errors.New("static_alipay refunds are not available in V0.1")
	}
	if item.Status != domain.StatusPaid && item.Status != domain.StatusPartiallyRefunded {
		return BeginRefundResult{}, errors.New("payment is not refundable")
	}
	if item.RefundedMinor+input.AmountMinor > item.AmountMinor {
		return BeginRefundResult{}, errors.New("refund amount exceeds paid amount")
	}
	refundID := mustID("ref")
	createdAt := time.Now().UTC()
	created := createdAt.Format(time.RFC3339Nano)
	recoveryAt := createdAt.Add(30 * time.Second)
	if _, err := tx.ExecContext(ctx, `INSERT INTO refunds(id,merchant_id,payment_id,merchant_refund_id,amount_minor,status,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, refundID, input.MerchantID, input.PaymentID, input.MerchantRefund, input.AmountMinor, "pending", recoveryAt.Format(time.RFC3339Nano), created, created); err != nil {
		return BeginRefundResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status='refunding',updated_at=? WHERE id=?`, created, input.PaymentID); err != nil {
		return BeginRefundResult{}, err
	}
	item.Status = domain.StatusRefunding
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), input.MerchantID, actor, "refund.created", "refund", refundID, "{}", created); err != nil {
		return BeginRefundResult{}, err
	}
	refund := domain.Refund{ID: refundID, MerchantID: input.MerchantID, PaymentID: input.PaymentID, MerchantRefund: input.MerchantRefund, AmountMinor: input.AmountMinor, Status: "pending", NextAttemptAt: &recoveryAt, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := tx.Commit(); err != nil {
		return BeginRefundResult{}, err
	}
	return BeginRefundResult{Refund: refund, Payment: item, Created: true}, nil
}

func (s *Store) CompleteRefund(ctx context.Context, merchantID, refundID, channelRefundID string) (domain.Refund, domain.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	defer tx.Rollback()
	refund, payment, err := s.refundAndPaymentTx(ctx, tx, merchantID, refundID)
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	if refund.Status == "succeeded" {
		return refund, payment, tx.Commit()
	}
	updated := now()
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status='succeeded',channel_refund_id=?,last_error='',next_attempt_at=NULL,updated_at=? WHERE id=?`, channelRefundID, updated, refundID); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	payment.RefundedMinor += refund.AmountMinor
	newStatus := domain.StatusPartiallyRefunded
	if payment.RefundedMinor == payment.AmountMinor {
		newStatus = domain.StatusRefunded
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status=?,refunded_minor=?,last_error='',updated_at=? WHERE id=?`, newStatus, payment.RefundedMinor, updated, payment.ID); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	payment.Status = newStatus
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, "system", "refund.succeeded", "refund", refundID, "{}", updated); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	payload, _ := json.Marshal(map[string]any{"id": refund.ID, "payment_id": payment.ID, "merchant_refund_id": refund.MerchantRefund, "amount_minor": refund.AmountMinor, "status": "succeeded", "event_type": "refund.updated"})
	if err := s.enqueuePayloadTx(ctx, tx, merchantID, payment.ApplicationID, "refund.updated", payment.ID, payload, updated); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	refund.Status = "succeeded"
	refund.ChannelRefund = channelRefundID
	refund.UpdatedAt = parseTime(sql.NullString{String: updated, Valid: true})
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	return refund, payment, nil
}

func (s *Store) FailRefund(ctx context.Context, merchantID, refundID, reason string) (domain.Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, err
	}
	defer tx.Rollback()
	refund, payment, err := s.refundAndPaymentTx(ctx, tx, merchantID, refundID)
	if err != nil {
		return domain.Refund{}, err
	}
	if refund.Status == "failed" || refund.Status == "succeeded" {
		return refund, tx.Commit()
	}
	updated := now()
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status='failed',last_error=?,next_attempt_at=NULL,updated_at=? WHERE id=?`, reason, updated, refundID); err != nil {
		return domain.Refund{}, err
	}
	previous := domain.StatusPaid
	if payment.RefundedMinor > 0 {
		previous = domain.StatusPartiallyRefunded
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status=?,last_error=?,updated_at=? WHERE id=?`, previous, reason, updated, payment.ID); err != nil {
		return domain.Refund{}, err
	}
	refund.Status = "failed"
	refund.LastError = reason
	refund.UpdatedAt = parseTime(sql.NullString{String: updated, Valid: true})
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, err
	}
	return refund, nil
}

func (s *Store) refundAndPaymentTx(ctx context.Context, tx *sql.Tx, merchantID, refundID string) (domain.Refund, domain.Payment, error) {
	refund, err := scanRefund(tx.QueryRowContext(ctx, refundSelect+` WHERE merchant_id=? AND id=?`, merchantID, refundID))
	if errorsIsNoRows(err) {
		return domain.Refund{}, domain.Payment{}, ErrNotFound
	}
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	payment, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` WHERE merchant_id=? AND id=?`, merchantID, refund.PaymentID))
	if errorsIsNoRows(err) {
		return domain.Refund{}, domain.Payment{}, ErrNotFound
	}
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	return refund, payment, nil
}

func (s *Store) GetRefundAndPayment(ctx context.Context, merchantID, refundID string) (domain.Refund, domain.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	defer tx.Rollback()
	refund, payment, err := s.refundAndPaymentTx(ctx, tx, merchantID, refundID)
	if err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, domain.Payment{}, err
	}
	return refund, payment, nil
}

func (s *Store) MarkRefundUnknown(ctx context.Context, merchantID, refundID, reason string) (domain.Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Refund{}, err
	}
	defer tx.Rollback()
	refund, payment, err := s.refundAndPaymentTx(ctx, tx, merchantID, refundID)
	if err != nil {
		return domain.Refund{}, err
	}
	if refund.Status != "pending" {
		return refund, tx.Commit()
	}
	attempts := refund.Attempts + 1
	updatedAt := time.Now().UTC()
	nextAttemptAt := updatedAt.Add(retryBackoff(attempts))
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET last_error=?,attempts=?,next_attempt_at=?,updated_at=? WHERE id=? AND status='pending'`, reason, attempts, nextAttemptAt.Format(time.RFC3339Nano), updatedAt.Format(time.RFC3339Nano), refundID); err != nil {
		return domain.Refund{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET last_error=?,updated_at=? WHERE id=? AND status='refunding'`, reason, updatedAt.Format(time.RFC3339Nano), payment.ID); err != nil {
		return domain.Refund{}, err
	}
	refund.LastError = reason
	refund.Attempts = attempts
	refund.NextAttemptAt = &nextAttemptAt
	refund.UpdatedAt = updatedAt
	if err := tx.Commit(); err != nil {
		return domain.Refund{}, err
	}
	return refund, nil
}

type DueRefund struct {
	MerchantID string
	RefundID   string
}

func (s *Store) ListDueRefunds(ctx context.Context, limit int) ([]DueRefund, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT merchant_id,id FROM refunds WHERE status='pending' AND next_attempt_at IS NOT NULL AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT ?`, now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DueRefund
	for rows.Next() {
		var item DueRefund
		if err := rows.Scan(&item.MerchantID, &item.RefundID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListPayments(ctx context.Context, merchantID, status string, limit int) ([]domain.Payment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := paymentSelect + ` WHERE merchant_id=?`
	args := []any{merchantID}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Payment
	for rows.Next() {
		item, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListAudits(ctx context.Context, merchantID string, limit int) ([]domain.AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,merchant_id,actor,action,resource_type,resource_id,details,created_at FROM audit_logs WHERE merchant_id=? ORDER BY created_at DESC LIMIT ?`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AuditLog
	for rows.Next() {
		var item domain.AuditLog
		var created string
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.Actor, &item.Action, &item.ResourceType, &item.ResourceID, &item.Details, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListFees(ctx context.Context, merchantID string, limit int) ([]domain.Fee, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,merchant_id,payment_id,kind,amount_minor,currency,created_at FROM fees WHERE merchant_id=? ORDER BY created_at DESC LIMIT ?`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Fee
	for rows.Next() {
		var item domain.Fee
		var created string
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.PaymentID, &item.Kind, &item.AmountMinor, &item.Currency, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListRefunds(ctx context.Context, merchantID string, limit int) ([]domain.Refund, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, refundSelect+` WHERE merchant_id=? ORDER BY created_at DESC LIMIT ?`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Refund
	for rows.Next() {
		item, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func mustID(prefix string) string {
	id, err := security.NewID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

func isUniqueError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint")
}
