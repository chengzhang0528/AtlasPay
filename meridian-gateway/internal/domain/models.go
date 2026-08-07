package domain

import (
	"errors"
	"fmt"
	"time"
)

type PaymentStatus string

const (
	StatusCreated           PaymentStatus = "created"
	StatusPending           PaymentStatus = "pending"
	StatusPaid              PaymentStatus = "paid"
	StatusFailed            PaymentStatus = "failed"
	StatusClosed            PaymentStatus = "closed"
	StatusRefunding         PaymentStatus = "refunding"
	StatusPartiallyRefunded PaymentStatus = "partially_refunded"
	StatusRefunded          PaymentStatus = "refunded"
)

type PaymentMethod string

const (
	MethodStaticAlipay PaymentMethod = "static_alipay"
	MethodAlipayQR     PaymentMethod = "alipay_qr"
	MethodAlipayPage   PaymentMethod = "alipay_page"
	MethodAlipayWAP    PaymentMethod = "alipay_wap"
)

type AlipayEnvironment string

const (
	AlipayEnvironmentSandbox    AlipayEnvironment = "sandbox"
	AlipayEnvironmentProduction AlipayEnvironment = "production"
)

type Merchant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	FeeBPS    int       `json:"fee_bps"`
	CreatedAt time.Time `json:"created_at"`
}

type Application struct {
	ID            string    `json:"id"`
	MerchantID    string    `json:"merchant_id"`
	Name          string    `json:"name"`
	WebhookURL    string    `json:"webhook_url,omitempty"`
	WebhookSecret string    `json:"-"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type AlipayCredentials struct {
	AppID              string `json:"app_id"`
	PrivateKeyPEM      string `json:"private_key_pem"`
	AlipayPublicKeyPEM string `json:"alipay_public_key_pem"`
}

type AlipayAccount struct {
	ID                 string            `json:"id"`
	MerchantID         string            `json:"merchant_id"`
	AppID              string            `json:"app_id"`
	Environment        AlipayEnvironment `json:"environment"`
	GatewayURL         string            `json:"-"`
	NotifyURL          string            `json:"notify_url"`
	HasPrivateKey      bool              `json:"has_private_key"`
	HasAlipayPublicKey bool              `json:"has_alipay_public_key"`
	Status             string            `json:"status"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type StaticCode struct {
	ID         string    `json:"id"`
	MerchantID string    `json:"merchant_id"`
	Name       string    `json:"name"`
	StoreName  string    `json:"store_name"`
	Currency   string    `json:"currency"`
	MIMEType   string    `json:"mime_type,omitempty"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

type Payment struct {
	ID              string        `json:"id"`
	MerchantID      string        `json:"merchant_id"`
	ApplicationID   string        `json:"application_id"`
	MerchantOrderID string        `json:"merchant_order_id"`
	Method          PaymentMethod `json:"method"`
	AmountMinor     int64         `json:"amount_minor"`
	Currency        string        `json:"currency"`
	Subject         string        `json:"subject"`
	Status          PaymentStatus `json:"status"`
	StaticCodeID    string        `json:"static_code_id,omitempty"`
	CheckoutToken   string        `json:"-"`
	ChannelOrderID  string        `json:"channel_order_id,omitempty"`
	ChannelStatus   string        `json:"channel_status,omitempty"`
	QRContent       string        `json:"-"`
	RedirectURL     string        `json:"redirect_url,omitempty"`
	ReturnURL       string        `json:"return_url,omitempty"`
	ExpiresAt       *time.Time    `json:"expires_at,omitempty"`
	PaidAt          *time.Time    `json:"paid_at,omitempty"`
	ClosedAt        *time.Time    `json:"closed_at,omitempty"`
	RefundedMinor   int64         `json:"refunded_minor"`
	LastError       string        `json:"last_error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

func (p Payment) IsPayable(at time.Time) bool {
	if p.Status != StatusCreated && p.Status != StatusPending {
		return false
	}
	return p.ExpiresAt == nil || p.ExpiresAt.After(at)
}

type Refund struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	PaymentID      string     `json:"payment_id"`
	MerchantRefund string     `json:"merchant_refund_id"`
	AmountMinor    int64      `json:"amount_minor"`
	Status         string     `json:"status"`
	ChannelRefund  string     `json:"channel_refund_id,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Fee struct {
	ID          string    `json:"id"`
	MerchantID  string    `json:"merchant_id"`
	PaymentID   string    `json:"payment_id"`
	Kind        string    `json:"kind"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

type Delivery struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	ApplicationID  string     `json:"application_id"`
	EventType      string     `json:"event_type"`
	AggregateID    string     `json:"aggregate_id"`
	Payload        []byte     `json:"payload,omitempty"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LastError      string     `json:"last_error,omitempty"`
	LastStatusCode int        `json:"last_status_code,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

type AuditLog struct {
	ID           string    `json:"id"`
	MerchantID   string    `json:"merchant_id"`
	Actor        string    `json:"actor"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Details      string    `json:"details,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func ValidatePaymentInput(orderID string, method PaymentMethod, amount int64, currency, subject string) error {
	if len(orderID) < 1 || len(orderID) > 96 {
		return errors.New("merchant_order_id must contain 1-96 characters")
	}
	if amount <= 0 || amount > 100_000_000_00 {
		return errors.New("amount_minor must be between 1 and 10000000000")
	}
	if currency != "CNY" {
		return errors.New("currency must be CNY")
	}
	if len(subject) < 1 || len(subject) > 128 {
		return errors.New("subject must contain 1-128 characters")
	}
	switch method {
	case MethodStaticAlipay, MethodAlipayQR, MethodAlipayPage, MethodAlipayWAP:
		return nil
	default:
		return fmt.Errorf("unsupported payment method %q", method)
	}
}

func CanTransition(from, to PaymentStatus) bool {
	if from == to {
		return true
	}
	allowed := map[PaymentStatus]map[PaymentStatus]bool{
		StatusCreated: {
			StatusPending: true,
			StatusFailed:  true,
		},
		StatusPending: {
			StatusPaid:   true,
			StatusFailed: true,
			StatusClosed: true,
		},
		StatusPaid: {
			StatusRefunding:         true,
			StatusPartiallyRefunded: true,
			StatusRefunded:          true,
		},
		StatusRefunding: {
			StatusPaid:              true,
			StatusPartiallyRefunded: true,
			StatusRefunded:          true,
		},
		StatusPartiallyRefunded: {
			StatusRefunding: true,
			StatusRefunded:  true,
		},
	}
	return allowed[from][to]
}
