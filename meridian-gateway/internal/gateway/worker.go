package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/security"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

type Worker struct {
	Store      *storage.Store
	Service    *Service
	Interval   time.Duration
	HTTPClient *http.Client
	Logger     *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	if err := w.deliver(ctx); err != nil {
		w.log("webhook worker", err)
	}
	if err := w.closeExpired(ctx); err != nil {
		w.log("expiry worker", err)
	}
	if err := w.reconcileRefunds(ctx); err != nil {
		w.log("refund worker", err)
	}
}

func (w *Worker) deliver(ctx context.Context) error {
	items, err := w.Store.ClaimDueDeliveries(ctx, 20)
	if err != nil {
		return err
	}
	client := w.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	for _, item := range items {
		timestamp := time.Now().UTC()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.WebhookURL, bytes.NewReader(item.Payload))
		if err != nil {
			_ = w.Store.MarkDeliveryFailure(ctx, item.ID, err.Error(), 0, item.Attempts)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Meridian-Gateway/0.1")
		req.Header.Set("X-Meridian-Event", item.EventType)
		req.Header.Set("X-Meridian-Delivery", item.ID)
		req.Header.Set("X-Meridian-Timestamp", strconv.FormatInt(timestamp.Unix(), 10))
		req.Header.Set("X-Meridian-Signature", "v1="+security.WebhookSignature(item.WebhookSecret, timestamp, item.Payload))
		resp, err := client.Do(req)
		if err != nil {
			_ = w.Store.MarkDeliveryFailure(ctx, item.ID, err.Error(), 0, item.Attempts)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if readErr != nil {
			_ = w.Store.MarkDeliveryFailure(ctx, item.ID, readErr.Error(), resp.StatusCode, item.Attempts)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = w.Store.MarkDeliverySuccess(ctx, item.ID, resp.StatusCode)
			continue
		}
		_ = w.Store.MarkDeliveryFailure(ctx, item.ID, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), resp.StatusCode, item.Attempts)
	}
	return nil
}

func (w *Worker) closeExpired(ctx context.Context) error {
	items, err := w.Store.ListExpiredPayments(ctx, 50)
	if err != nil {
		return err
	}
	for _, item := range items {
		var err error
		if w.Service != nil {
			err = w.Service.CloseExpiredPayment(ctx, item.MerchantID, item.PaymentID)
		} else {
			_, err = w.Store.ClosePayment(ctx, item.MerchantID, item.PaymentID, "system.expiry")
		}
		if err != nil && err != storage.ErrInvalidState {
			w.log("close expired payment", err)
			if scheduleErr := w.Store.SchedulePaymentCloseRetry(ctx, item.MerchantID, item.PaymentID, err.Error()); scheduleErr != nil && scheduleErr != storage.ErrInvalidState {
				w.log("schedule expired payment retry", scheduleErr)
			}
		}
	}
	return nil
}

func (w *Worker) reconcileRefunds(ctx context.Context) error {
	if w.Service == nil {
		return nil
	}
	items, err := w.Store.ListDueRefunds(ctx, 50)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := w.Service.ReconcileRefund(ctx, item.MerchantID, item.RefundID); err != nil {
			w.log("reconcile refund", err)
		}
	}
	return nil
}

func (w *Worker) log(message string, err error) {
	if w.Logger != nil {
		w.Logger.Error(message, "error", err)
	}
}
