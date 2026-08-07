package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/storage"
)

var (
	ErrNotFound     = storage.ErrNotFound
	ErrConflict     = storage.ErrConflict
	ErrInvalidState = storage.ErrInvalidState
)

type AlipayCredentials = domain.AlipayCredentials

const alipayNotifyPath = "/v1/webhooks/alipay"

func (s *Service) Authenticate(ctx context.Context, apiKey string) (domain.Application, error) {
	return s.Store.GetApplicationByAPIKey(ctx, apiKey)
}
func (s *Service) AuthenticateMerchantAdmin(ctx context.Context, token string) (domain.Merchant, error) {
	return s.Store.GetMerchantByAdminToken(ctx, token)
}
func (s *Service) ListMerchants(ctx context.Context) ([]domain.Merchant, error) {
	return s.Store.ListMerchants(ctx)
}
func (s *Service) Merchant(ctx context.Context, merchantID string) (domain.Merchant, error) {
	return s.Store.GetMerchant(ctx, merchantID)
}
func (s *Service) CreateMerchant(ctx context.Context, name string, feeBPS int) (domain.Merchant, error) {
	return s.Store.CreateMerchant(ctx, name, feeBPS)
}
func (s *Service) RotateMerchantAdminToken(ctx context.Context, merchantID, actor string) (string, error) {
	return s.Store.RotateMerchantAdminToken(ctx, merchantID, actor)
}
func (s *Service) ListApplications(ctx context.Context, merchantID string) ([]domain.Application, error) {
	return s.Store.ListApplications(ctx, merchantID)
}
func (s *Service) CreateApplication(ctx context.Context, merchantID, name, webhookURL string) (domain.Application, string, error) {
	if err := validateWebhookURL(webhookURL); err != nil {
		return domain.Application{}, "", err
	}
	return s.Store.CreateApplication(ctx, merchantID, name, webhookURL)
}
func (s *Service) UpdateApplication(ctx context.Context, merchantID, id, name, webhookURL, status, actor string) (domain.Application, error) {
	if err := validateWebhookURL(webhookURL); err != nil {
		return domain.Application{}, err
	}
	if status != "active" && status != "disabled" {
		return domain.Application{}, errors.New("status must be active or disabled")
	}
	return s.Store.UpdateApplication(ctx, merchantID, id, name, webhookURL, status, actor)
}
func (s *Service) RotateApplicationCredentials(ctx context.Context, merchantID, id, actor string) (domain.Application, string, error) {
	return s.Store.RotateApplicationCredentials(ctx, merchantID, id, actor)
}
func (s *Service) SaveAlipayAccount(ctx context.Context, merchantID string, environment domain.AlipayEnvironment, credentials domain.AlipayCredentials) (domain.AlipayAccount, error) {
	environment, gatewayURL, err := alipay.ResolveEnvironment(environment)
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	notifyURL, err := s.alipayNotifyURL()
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	if _, err := alipay.NewClient(credentials, gatewayURL, notifyURL); err != nil {
		return domain.AlipayAccount{}, err
	}
	item, err := s.Store.PutAlipayAccount(ctx, merchantID, gatewayURL, notifyURL, credentials)
	item.Environment = environment
	return item, err
}
func (s *Service) AlipayAccount(ctx context.Context, merchantID string) (domain.AlipayAccount, error) {
	item, _, err := s.Store.GetAlipayAccount(ctx, merchantID)
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	item.Environment, err = alipay.EnvironmentForGatewayURL(item.GatewayURL)
	return item, err
}

func (s *Service) alipayNotifyURL() (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if baseURL == "" {
		return "", errors.New("MERIDIAN_BASE_URL is required for Alipay notifications")
	}
	notifyURL := baseURL + alipayNotifyPath
	if err := validatePublicURL(notifyURL); err != nil {
		return "", fmt.Errorf("MERIDIAN_BASE_URL: %w", err)
	}
	return notifyURL, nil
}
func (s *Service) CreateStaticCode(ctx context.Context, merchantID, name, storeName, currency, mime string, image []byte) (domain.StaticCode, error) {
	return s.Store.CreateStaticCode(ctx, merchantID, name, storeName, currency, mime, image)
}
func (s *Service) ListStaticCodes(ctx context.Context, merchantID string) ([]domain.StaticCode, error) {
	return s.Store.ListStaticCodes(ctx, merchantID)
}
func (s *Service) SetStaticCodeEnabled(ctx context.Context, merchantID, id string, enabled bool) error {
	return s.Store.SetStaticCodeEnabled(ctx, merchantID, id, enabled)
}
func (s *Service) AdminPayments(ctx context.Context, merchantID, status string, limit int) ([]domain.Payment, error) {
	return s.Store.ListPayments(ctx, merchantID, status, limit)
}
func (s *Service) AdminCreatePayment(ctx context.Context, merchantID, applicationID string, input CreatePaymentInput) (PaymentView, bool, error) {
	app, err := s.Store.GetApplication(ctx, merchantID, applicationID)
	if err != nil {
		return PaymentView{}, false, err
	}
	if app.Status != "active" {
		return PaymentView{}, false, ErrInvalidState
	}
	return s.CreatePayment(ctx, app, input)
}
func (s *Service) ConfirmPayment(ctx context.Context, merchantID, id, actor, note string) (domain.Payment, error) {
	if len(note) > 1000 || len(actor) > 128 {
		return domain.Payment{}, errors.New("actor or note is too long")
	}
	return s.Store.ConfirmPayment(ctx, merchantID, id, actor, note)
}
func (s *Service) MarkAbnormal(ctx context.Context, merchantID, id, actor, note string) error {
	if len(note) > 1000 || len(actor) > 128 {
		return errors.New("actor or note is too long")
	}
	return s.Store.MarkAbnormal(ctx, merchantID, id, actor, note)
}
func (s *Service) AdminQueryPayment(ctx context.Context, merchantID, id string) (PaymentView, error) {
	payment, err := s.Store.GetPayment(ctx, merchantID, id)
	if err != nil {
		return PaymentView{}, err
	}
	app, err := s.Store.GetApplication(ctx, merchantID, payment.ApplicationID)
	if err != nil {
		return PaymentView{}, err
	}
	return s.QueryPayment(ctx, app, id)
}
func (s *Service) AdminClosePayment(ctx context.Context, merchantID, id string) (PaymentView, error) {
	payment, err := s.Store.GetPayment(ctx, merchantID, id)
	if err != nil {
		return PaymentView{}, err
	}
	app, err := s.Store.GetApplication(ctx, merchantID, payment.ApplicationID)
	if err != nil {
		return PaymentView{}, err
	}
	return s.ClosePayment(ctx, app, id, "admin")
}
func (s *Service) AdminRefund(ctx context.Context, merchantID, id, merchantRefundID string, amount int64, reason string) (domain.Refund, error) {
	payment, err := s.Store.GetPayment(ctx, merchantID, id)
	if err != nil {
		return domain.Refund{}, err
	}
	app, err := s.Store.GetApplication(ctx, merchantID, payment.ApplicationID)
	if err != nil {
		return domain.Refund{}, err
	}
	return s.Refund(ctx, app, id, merchantRefundID, amount, reason)
}
func (s *Service) Deliveries(ctx context.Context, merchantID string, limit int) ([]domain.Delivery, error) {
	return s.Store.ListDeliveries(ctx, merchantID, limit)
}
func (s *Service) RetryDelivery(ctx context.Context, merchantID, id, actor string) error {
	delivery, err := s.Store.GetDelivery(ctx, merchantID, id)
	if err != nil {
		return err
	}
	app, err := s.Store.GetApplication(ctx, merchantID, delivery.ApplicationID)
	if err != nil {
		return err
	}
	if app.Status != "active" {
		return errors.New("application must be active before retrying delivery")
	}
	if strings.TrimSpace(app.WebhookURL) == "" {
		return errors.New("configure the application webhook URL before retrying delivery")
	}
	return s.Store.RetryDelivery(ctx, merchantID, id, actor)
}
func (s *Service) Audits(ctx context.Context, merchantID string, limit int) ([]domain.AuditLog, error) {
	return s.Store.ListAudits(ctx, merchantID, limit)
}
func (s *Service) Fees(ctx context.Context, merchantID string, limit int) ([]domain.Fee, error) {
	return s.Store.ListFees(ctx, merchantID, limit)
}
func (s *Service) Refunds(ctx context.Context, merchantID string, limit int) ([]domain.Refund, error) {
	return s.Store.ListRefunds(ctx, merchantID, limit)
}
func (s *Service) CheckoutPayment(ctx context.Context, token string) (domain.Payment, error) {
	return s.Store.GetPaymentByCheckoutToken(ctx, token)
}
func (s *Service) ResultPayment(ctx context.Context, id string) (domain.Payment, error) {
	return s.Store.GetPaymentByID(ctx, id)
}
func (s *Service) StaticCodeImage(ctx context.Context, merchantID, id string) (domain.StaticCode, []byte, error) {
	return s.Store.GetStaticCode(ctx, merchantID, id)
}

func validateWebhookURL(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("webhook_url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("webhook_url must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return errors.New("webhook_url must not include user information")
	}
	return nil
}

func validatePublicURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("must use HTTP or HTTPS")
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return errors.New("unspecified host is not allowed")
	}
	return nil
}
