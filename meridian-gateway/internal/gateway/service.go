package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

type Service struct {
	Store               *storage.Store
	BaseURL             string
	AlipayClientFactory func(context.Context, string) (AlipayClient, domain.AlipayAccount, error)
}

type AlipayClient interface {
	Precreate(context.Context, string, int64, string, string) (alipay.PaymentResult, error)
	Query(context.Context, string, string) (alipay.PaymentResult, error)
	Close(context.Context, string, string) (alipay.PaymentResult, error)
	Refund(context.Context, string, string, string, int64, string) (alipay.PaymentResult, error)
	QueryRefund(context.Context, string, string, string) (alipay.PaymentResult, error)
	BuildPayURLWithTimeout(string, string, int64, string, string, string) (string, error)
	VerifyNotification(url.Values) (alipay.WebhookEvent, error)
}

type CreatePaymentInput struct {
	MerchantOrderID string
	Method          domain.PaymentMethod
	AmountMinor     int64
	Currency        string
	Subject         string
	StaticCodeID    string
	ReturnURL       string
	ExpiresIn       time.Duration
}

type PaymentView struct {
	domain.Payment
	CheckoutURL string `json:"checkout_url"`
	QRURL       string `json:"qr_url,omitempty"`
}

func (s *Service) CreatePayment(ctx context.Context, app domain.Application, input CreatePaymentInput) (PaymentView, bool, error) {
	if input.ReturnURL != "" {
		if err := validatePublicURL(input.ReturnURL); err != nil {
			return PaymentView{}, false, fmt.Errorf("return_url: %w", err)
		}
	}
	if input.ExpiresIn <= 0 {
		input.ExpiresIn = 30 * time.Minute
	}
	if input.ExpiresIn < time.Minute || input.ExpiresIn > 24*time.Hour {
		return PaymentView{}, false, errors.New("expires_in_seconds must be between 60 and 86400")
	}
	expires := time.Now().UTC().Add(input.ExpiresIn)
	created, err := s.Store.CreatePayment(ctx, storage.CreatePaymentInput{MerchantID: app.MerchantID, ApplicationID: app.ID, MerchantOrderID: input.MerchantOrderID, Method: input.Method, AmountMinor: input.AmountMinor, Currency: input.Currency, Subject: input.Subject, StaticCodeID: input.StaticCodeID, ReturnURL: input.ReturnURL, ExpiresAt: &expires})
	if err != nil {
		return PaymentView{}, false, err
	}
	needsInitialization := created.Payment.Method != domain.MethodStaticAlipay && (created.Created || created.Payment.Status == domain.StatusCreated)
	if needsInitialization && (created.Payment.ExpiresAt == nil || created.Payment.ExpiresAt.After(time.Now().UTC())) {
		retryable, err := s.initializeAlipayPayment(ctx, &created.Payment)
		if err != nil {
			if retryable {
				_ = s.Store.SetPaymentLastError(ctx, app.MerchantID, created.Payment.ID, err.Error())
			} else {
				_ = s.Store.FailPayment(ctx, app.MerchantID, created.Payment.ID, err.Error())
			}
			return PaymentView{}, false, err
		}
		created.Payment, err = s.Store.GetPayment(ctx, app.MerchantID, created.Payment.ID)
		if err != nil {
			return PaymentView{}, false, err
		}
	}
	return s.view(created.Payment), created.Created, nil
}

func (s *Service) initializeAlipayPayment(ctx context.Context, payment *domain.Payment) (bool, error) {
	client, _, err := s.alipayClient(ctx, payment.MerchantID)
	if err != nil {
		return false, fmt.Errorf("alipay account is not ready: %w", err)
	}
	timeout := paymentTimeout(payment)
	switch payment.Method {
	case domain.MethodAlipayQR:
		result, err := client.Precreate(ctx, payment.ID, payment.AmountMinor, payment.Subject, timeout)
		if err != nil {
			return true, err
		}
		if err := s.Store.UpdatePaymentChannel(ctx, payment.MerchantID, payment.ID, result.TradeNo, "WAIT_BUYER_PAY", result.QRContent, ""); err != nil {
			return true, err
		}
		return false, nil
	case domain.MethodAlipayPage:
		redirect, err := client.BuildPayURLWithTimeout("alipay.trade.page.pay", payment.ID, payment.AmountMinor, payment.Subject, s.paymentReturnURL(payment), timeout)
		if err != nil {
			return false, err
		}
		if err := s.Store.UpdatePaymentChannel(ctx, payment.MerchantID, payment.ID, "", "WAIT_BUYER_PAY", "", redirect); err != nil {
			return true, err
		}
		return false, nil
	case domain.MethodAlipayWAP:
		redirect, err := client.BuildPayURLWithTimeout("alipay.trade.wap.pay", payment.ID, payment.AmountMinor, payment.Subject, s.paymentReturnURL(payment), timeout)
		if err != nil {
			return false, err
		}
		if err := s.Store.UpdatePaymentChannel(ctx, payment.MerchantID, payment.ID, "", "WAIT_BUYER_PAY", "", redirect); err != nil {
			return true, err
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported alipay payment method %s", payment.Method)
	}
}

func (s *Service) paymentReturnURL(payment *domain.Payment) string {
	if payment.ReturnURL != "" || s.BaseURL == "" {
		return payment.ReturnURL
	}
	return strings.TrimRight(s.BaseURL, "/") + "/result?payment_id=" + url.QueryEscape(payment.ID)
}

func paymentTimeout(payment *domain.Payment) string {
	if payment.ExpiresAt == nil {
		return "30m"
	}
	remaining := time.Until(*payment.ExpiresAt)
	minutes := (remaining + time.Minute - 1) / time.Minute
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}

func (s *Service) GetPayment(ctx context.Context, app domain.Application, id string) (PaymentView, error) {
	payment, err := s.Store.GetPayment(ctx, app.MerchantID, id)
	if err != nil {
		return PaymentView{}, err
	}
	if payment.ApplicationID != app.ID {
		return PaymentView{}, storage.ErrNotFound
	}
	return s.view(payment), nil
}

func (s *Service) QueryPayment(ctx context.Context, app domain.Application, id string) (PaymentView, error) {
	payment, err := s.Store.GetPayment(ctx, app.MerchantID, id)
	if err != nil {
		return PaymentView{}, err
	}
	if payment.ApplicationID != app.ID {
		return PaymentView{}, storage.ErrNotFound
	}
	if payment.Method == domain.MethodStaticAlipay || (payment.Status != domain.StatusPending && payment.Status != domain.StatusCreated) {
		return s.view(payment), nil
	}
	client, _, err := s.alipayClient(ctx, payment.MerchantID)
	if err != nil {
		return PaymentView{}, err
	}
	result, err := client.Query(ctx, payment.ID, payment.ChannelOrderID)
	if err != nil {
		_ = s.Store.SetPaymentLastError(ctx, payment.MerchantID, payment.ID, err.Error())
		return PaymentView{}, err
	}
	if result.TradeStatus == "TRADE_SUCCESS" || result.TradeStatus == "TRADE_FINISHED" {
		payment, _, err = s.Store.ApplyChannelPaid(ctx, payment.MerchantID, payment.ID, "alipay.query", result.TradeNo, result.TradeStatus, "query:"+result.TradeNo+":"+result.TradeStatus, "")
		if err != nil {
			return PaymentView{}, err
		}
	} else if result.TradeStatus == "TRADE_CLOSED" {
		payment, err = s.Store.ClosePayment(ctx, payment.MerchantID, payment.ID, "alipay.query")
		if err != nil {
			return PaymentView{}, err
		}
	} else {
		_ = s.Store.UpdatePaymentChannel(ctx, payment.MerchantID, payment.ID, result.TradeNo, result.TradeStatus, payment.QRContent, payment.RedirectURL)
		payment, err = s.Store.GetPayment(ctx, payment.MerchantID, payment.ID)
		if err != nil {
			return PaymentView{}, err
		}
	}
	return s.view(payment), nil
}

func (s *Service) ClosePayment(ctx context.Context, app domain.Application, id, actor string) (PaymentView, error) {
	payment, err := s.Store.GetPayment(ctx, app.MerchantID, id)
	if err != nil {
		return PaymentView{}, err
	}
	if payment.ApplicationID != app.ID {
		return PaymentView{}, storage.ErrNotFound
	}
	if payment.Status != domain.StatusCreated && payment.Status != domain.StatusPending && payment.Status != domain.StatusClosed {
		return PaymentView{}, storage.ErrInvalidState
	}
	if payment.Method != domain.MethodStaticAlipay && payment.Status != domain.StatusClosed {
		client, _, err := s.alipayClient(ctx, payment.MerchantID)
		if err != nil {
			return PaymentView{}, err
		}
		payment, closeLocally, err := s.prepareChannelClose(ctx, client, payment)
		if err != nil {
			return PaymentView{}, err
		}
		if !closeLocally {
			return s.view(payment), storage.ErrInvalidState
		}
	}
	payment, err = s.Store.ClosePayment(ctx, payment.MerchantID, payment.ID, actor)
	if err != nil {
		return PaymentView{}, err
	}
	return s.view(payment), nil
}

func (s *Service) CloseExpiredPayment(ctx context.Context, merchantID, id string) error {
	payment, err := s.Store.GetPayment(ctx, merchantID, id)
	if err != nil {
		return err
	}
	if payment.Method != domain.MethodStaticAlipay {
		client, _, err := s.alipayClient(ctx, merchantID)
		if err != nil {
			return err
		}
		_, closeLocally, err := s.prepareChannelClose(ctx, client, payment)
		if err != nil {
			return err
		}
		if !closeLocally {
			return nil
		}
	}
	_, err = s.Store.ClosePayment(ctx, merchantID, id, "system.expiry")
	return err
}

func (s *Service) prepareChannelClose(ctx context.Context, client AlipayClient, payment domain.Payment) (domain.Payment, bool, error) {
	result, err := client.Query(ctx, payment.ID, payment.ChannelOrderID)
	if err != nil {
		if alipay.IsTradeNotExist(err) {
			return payment, true, nil
		}
		return domain.Payment{}, false, err
	}
	if result.TradeStatus == "TRADE_SUCCESS" || result.TradeStatus == "TRADE_FINISHED" {
		paid, _, err := s.Store.ApplyChannelPaid(ctx, payment.MerchantID, payment.ID, "alipay.query-before-close", result.TradeNo, result.TradeStatus, "query:"+result.TradeNo+":"+result.TradeStatus, "")
		return paid, false, err
	}
	if result.TradeStatus == "TRADE_CLOSED" {
		return payment, true, nil
	}
	if _, err := client.Close(ctx, payment.ID, payment.ChannelOrderID); err != nil {
		if alipay.IsTradeNotExist(err) {
			return payment, true, nil
		}
		return domain.Payment{}, false, err
	}
	return payment, true, nil
}

func (s *Service) Refund(ctx context.Context, app domain.Application, paymentID, refundID string, amount int64, reason string) (domain.Refund, error) {
	begin, err := s.Store.BeginRefund(ctx, storage.RefundInput{MerchantID: app.MerchantID, ApplicationID: app.ID, PaymentID: paymentID, MerchantRefund: refundID, AmountMinor: amount}, "api:"+app.ID)
	if err != nil {
		return domain.Refund{}, err
	}
	refund, payment := begin.Refund, begin.Payment
	if refund.Status != "pending" {
		return refund, nil
	}
	if !begin.Created && refund.LastError == "" && refund.NextAttemptAt != nil && refund.NextAttemptAt.After(time.Now().UTC()) {
		return refund, nil
	}
	client, _, err := s.alipayClient(ctx, payment.MerchantID)
	if err != nil {
		if begin.Created {
			_, _ = s.Store.FailRefund(ctx, payment.MerchantID, refund.ID, err.Error())
		} else {
			_, _ = s.Store.MarkRefundUnknown(ctx, payment.MerchantID, refund.ID, err.Error())
		}
		return domain.Refund{}, err
	}
	if !begin.Created {
		return s.reconcileRefund(ctx, client, refund, payment, reason)
	}
	return s.executeRefund(ctx, client, refund, payment, reason)
}

func (s *Service) ReconcileRefund(ctx context.Context, merchantID, refundID string) error {
	refund, payment, err := s.Store.GetRefundAndPayment(ctx, merchantID, refundID)
	if err != nil || refund.Status != "pending" {
		return err
	}
	client, _, err := s.alipayClient(ctx, merchantID)
	if err != nil {
		_, _ = s.Store.MarkRefundUnknown(ctx, merchantID, refundID, err.Error())
		return err
	}
	_, err = s.reconcileRefund(ctx, client, refund, payment, "")
	return err
}

func (s *Service) reconcileRefund(ctx context.Context, client AlipayClient, refund domain.Refund, payment domain.Payment, reason string) (domain.Refund, error) {
	result, err := client.QueryRefund(ctx, payment.ID, payment.ChannelOrderID, refund.MerchantRefund)
	if err == nil {
		if result.OutRequestNo != "" && result.OutRequestNo != refund.MerchantRefund {
			err = errors.New("alipay refund query out_request_no mismatch")
		} else if result.RefundAmount != 0 && result.RefundAmount != refund.AmountMinor {
			err = errors.New("alipay refund query amount mismatch")
		} else {
			channelRefundID := result.OutRequestNo
			if channelRefundID == "" {
				channelRefundID = refund.MerchantRefund
			}
			completed, _, completeErr := s.Store.CompleteRefund(ctx, payment.MerchantID, refund.ID, channelRefundID)
			return completed, completeErr
		}
	}
	if err != nil && !alipay.IsRefundNotExist(err) {
		pending, markErr := s.Store.MarkRefundUnknown(ctx, payment.MerchantID, refund.ID, err.Error())
		if markErr != nil {
			return domain.Refund{}, errors.Join(err, markErr)
		}
		return pending, nil
	}
	return s.executeRefund(ctx, client, refund, payment, reason)
}

func (s *Service) executeRefund(ctx context.Context, client AlipayClient, refund domain.Refund, payment domain.Payment, reason string) (domain.Refund, error) {
	result, err := client.Refund(ctx, payment.ID, payment.ChannelOrderID, refund.MerchantRefund, refund.AmountMinor, reason)
	if err != nil {
		if alipay.IsBusinessError(err) && !alipay.IsUncertainBusinessError(err) {
			failed, failErr := s.Store.FailRefund(ctx, payment.MerchantID, refund.ID, err.Error())
			if failErr != nil {
				return domain.Refund{}, errors.Join(err, failErr)
			}
			return failed, err
		}
		pending, markErr := s.Store.MarkRefundUnknown(ctx, payment.MerchantID, refund.ID, err.Error())
		if markErr != nil {
			return domain.Refund{}, errors.Join(err, markErr)
		}
		return pending, nil
	}
	channelRefundID := result.OutRequestNo
	if channelRefundID == "" {
		channelRefundID = refund.MerchantRefund
	}
	completed, _, err := s.Store.CompleteRefund(ctx, payment.MerchantID, refund.ID, channelRefundID)
	return completed, err
}

func (s *Service) HandleAlipayNotification(ctx context.Context, values url.Values) error {
	paymentID := strings.TrimSpace(values.Get("out_trade_no"))
	if paymentID == "" {
		return errors.New("out_trade_no is required")
	}
	payment, err := s.Store.GetPaymentByID(ctx, paymentID)
	if err != nil {
		return err
	}
	client, account, err := s.alipayClient(ctx, payment.MerchantID)
	if err != nil {
		return err
	}
	event, err := client.VerifyNotification(values)
	if err != nil {
		return err
	}
	if values.Get("app_id") != "" && values.Get("app_id") != account.AppID {
		return errors.New("alipay notification app_id mismatch")
	}
	if event.OutTradeNo != payment.ID || event.TotalAmount != payment.AmountMinor {
		return errors.New("alipay notification order or amount mismatch")
	}
	if event.TradeNo != "" && payment.ChannelOrderID != "" && event.TradeNo != payment.ChannelOrderID {
		return errors.New("alipay notification trade_no mismatch")
	}
	if event.TradeStatus == "TRADE_SUCCESS" || event.TradeStatus == "TRADE_FINISHED" {
		_, _, err = s.Store.ApplyChannelPaid(ctx, payment.MerchantID, payment.ID, "alipay.notify", event.TradeNo, event.TradeStatus, event.EventKey, event.Raw)
		return err
	}
	if event.TradeStatus == "TRADE_CLOSED" {
		_, err = s.Store.ClosePayment(ctx, payment.MerchantID, payment.ID, "alipay.notify")
		if errors.Is(err, storage.ErrInvalidState) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) alipayClient(ctx context.Context, merchantID string) (AlipayClient, domain.AlipayAccount, error) {
	if s.AlipayClientFactory != nil {
		return s.AlipayClientFactory(ctx, merchantID)
	}
	account, credentials, err := s.Store.GetAlipayAccount(ctx, merchantID)
	if err != nil {
		return nil, domain.AlipayAccount{}, err
	}
	account.Environment, err = alipay.EnvironmentForGatewayURL(account.GatewayURL)
	if err != nil {
		return nil, domain.AlipayAccount{}, err
	}
	client, err := alipay.NewClient(credentials, account.GatewayURL, account.NotifyURL)
	return client, account, err
}

func (s *Service) view(payment domain.Payment) PaymentView {
	view := PaymentView{Payment: payment, CheckoutURL: s.BaseURL + "/checkout/" + payment.CheckoutToken}
	if payment.QRContent != "" {
		view.QRURL = s.BaseURL + "/checkout/" + payment.CheckoutToken + "/qr.png"
	}
	if payment.RedirectURL != "" {
		view.RedirectURL = s.BaseURL + "/checkout/" + payment.CheckoutToken + "/redirect"
	}
	return view
}
