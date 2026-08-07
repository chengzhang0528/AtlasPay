package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/domain"
)

type DeliveryTarget struct {
	domain.Delivery
	WebhookURL    string
	WebhookSecret string
}

func (s *Store) ClaimDueDeliveries(ctx context.Context, limit int) ([]DeliveryTarget, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	claimTime := now()
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.merchant_id,o.application_id,o.event_type,o.aggregate_id,o.payload,o.status,o.attempts,o.next_attempt_at,o.last_error,o.last_status_code,o.created_at,o.delivered_at,a.webhook_url,a.webhook_secret_cipher FROM outbox_deliveries o JOIN applications a ON a.id=o.application_id WHERE o.status IN ('pending','failed') AND o.next_attempt_at<=? ORDER BY o.created_at LIMIT ?`, claimTime, limit)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		target DeliveryTarget
		cipher string
	}
	var candidates []candidate
	for rows.Next() {
		var item DeliveryTarget
		var nextAttempt, created string
		var delivered sql.NullString
		var cipher string
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.ApplicationID, &item.EventType, &item.AggregateID, &item.Payload, &item.Status, &item.Attempts, &nextAttempt, &item.LastError, &item.LastStatusCode, &created, &delivered, &item.WebhookURL, &cipher); err != nil {
			rows.Close()
			return nil, err
		}
		item.NextAttemptAt = parseTime(sql.NullString{String: nextAttempt, Valid: true})
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		item.DeliveredAt = parseOptionalTime(delivered)
		candidates = append(candidates, candidate{target: item, cipher: cipher})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var result []DeliveryTarget
	for _, candidate := range candidates {
		item := candidate.target
		if item.WebhookURL == "" {
			if _, err := s.db.ExecContext(ctx, `UPDATE outbox_deliveries SET status='skipped',attempts=attempts+1,last_error='webhook_url is empty' WHERE id=? AND status IN ('pending','failed') AND next_attempt_at<=?`, item.ID, claimTime); err != nil {
				return nil, err
			}
			continue
		}
		// Reserve this item for this process and bound the reservation window.
		reservation := time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
		update, err := s.db.ExecContext(ctx, `UPDATE outbox_deliveries SET status='failed',attempts=attempts+1,next_attempt_at=? WHERE id=? AND status IN ('pending','failed') AND next_attempt_at<=?`, reservation, item.ID, claimTime)
		if err != nil {
			return nil, err
		}
		count, _ := update.RowsAffected()
		if count != 1 {
			continue
		}
		secret, err := s.decrypt("application:"+item.ApplicationID+":webhook_secret", candidate.cipher)
		if err != nil {
			return nil, err
		}
		item.WebhookSecret = secret
		item.Attempts++
		result = append(result, item)
	}
	return result, nil
}

func (s *Store) MarkDeliverySuccess(ctx context.Context, id string, statusCode int) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE outbox_deliveries SET status='delivered',last_error='',last_status_code=?,delivered_at=?,next_attempt_at=? WHERE id=?`, statusCode, now(), now(), id); err != nil {
		return err
	}
	return nil
}

func (s *Store) MarkDeliveryFailure(ctx context.Context, id, reason string, statusCode, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	backoff := time.Duration(math.Min(float64(60*time.Minute), float64(time.Second)*math.Pow(2, float64(attempts-1))))
	next := time.Now().UTC().Add(backoff).Format(time.RFC3339Nano)
	status := "failed"
	if attempts >= 12 {
		status = "exhausted"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE outbox_deliveries SET status=?,last_error=?,last_status_code=?,next_attempt_at=? WHERE id=?`, status, reason, statusCode, next, id)
	return err
}

func (s *Store) ListDeliveries(ctx context.Context, merchantID string, limit int) ([]domain.Delivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,merchant_id,application_id,event_type,aggregate_id,payload,status,attempts,next_attempt_at,last_error,last_status_code,created_at,delivered_at FROM outbox_deliveries WHERE merchant_id=? ORDER BY created_at DESC LIMIT ?`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Delivery
	for rows.Next() {
		var item domain.Delivery
		var nextAttempt, created string
		var delivered sql.NullString
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.ApplicationID, &item.EventType, &item.AggregateID, &item.Payload, &item.Status, &item.Attempts, &nextAttempt, &item.LastError, &item.LastStatusCode, &created, &delivered); err != nil {
			return nil, err
		}
		item.NextAttemptAt = parseTime(sql.NullString{String: nextAttempt, Valid: true})
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		item.DeliveredAt = parseOptionalTime(delivered)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetDelivery(ctx context.Context, merchantID, id string) (domain.Delivery, error) {
	var item domain.Delivery
	var nextAttempt, created string
	var delivered sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,application_id,event_type,aggregate_id,payload,status,attempts,next_attempt_at,last_error,last_status_code,created_at,delivered_at FROM outbox_deliveries WHERE merchant_id=? AND id=?`, merchantID, id).Scan(&item.ID, &item.MerchantID, &item.ApplicationID, &item.EventType, &item.AggregateID, &item.Payload, &item.Status, &item.Attempts, &nextAttempt, &item.LastError, &item.LastStatusCode, &created, &delivered)
	if errorsIsNoRows(err) {
		return domain.Delivery{}, ErrNotFound
	}
	if err != nil {
		return domain.Delivery{}, err
	}
	item.NextAttemptAt = parseTime(sql.NullString{String: nextAttempt, Valid: true})
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	item.DeliveredAt = parseOptionalTime(delivered)
	return item, nil
}

func (s *Store) RetryDelivery(ctx context.Context, merchantID, id, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	queuedAt := now()
	result, err := tx.ExecContext(ctx, `UPDATE outbox_deliveries SET status='pending',attempts=0,next_attempt_at=?,last_error='',last_status_code=0,delivered_at=NULL WHERE merchant_id=? AND id=? AND status IN ('skipped','failed','exhausted')`, queuedAt, merchantID, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "delivery.retried", "delivery", id, "{}", queuedAt); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := s.GetDelivery(ctx, merchantID, id); err != nil {
		return err
	}
	return ErrInvalidState
}

type ExpiredPayment struct {
	MerchantID string
	PaymentID  string
}

func (s *Store) ListExpiredPayments(ctx context.Context, limit int) ([]ExpiredPayment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT merchant_id,id FROM payments WHERE status IN ('created','pending') AND expires_at IS NOT NULL AND expires_at<=? AND (close_next_attempt_at IS NULL OR close_next_attempt_at<=?) ORDER BY expires_at,id LIMIT ?`, now(), now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ExpiredPayment
	for rows.Next() {
		var item ExpiredPayment
		if err := rows.Scan(&item.MerchantID, &item.PaymentID); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) SchedulePaymentCloseRetry(ctx context.Context, merchantID, id, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT close_attempts FROM payments WHERE merchant_id=? AND id=? AND status IN ('created','pending')`, merchantID, id).Scan(&attempts)
	if errorsIsNoRows(err) {
		return ErrInvalidState
	}
	if err != nil {
		return err
	}
	attempts++
	timestamp := time.Now().UTC()
	nextAttempt := timestamp.Add(retryBackoff(attempts)).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET close_attempts=?,close_next_attempt_at=?,last_error=?,updated_at=? WHERE merchant_id=? AND id=? AND status IN ('created','pending')`, attempts, nextAttempt, reason, timestamp.Format(time.RFC3339Nano), merchantID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_attempts(id,payment_id,merchant_id,channel,result,error,created_at) VALUES(?,?,?,?,?,?,?)`, mustID("att"), id, merchantID, "alipay", "close_failed", reason, timestamp.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Count(ctx context.Context, table string) (int, error) {
	allowed := map[string]bool{"merchants": true, "payments": true, "outbox_deliveries": true, "audit_logs": true}
	if !allowed[table] {
		return 0, fmt.Errorf("unsupported count table")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
