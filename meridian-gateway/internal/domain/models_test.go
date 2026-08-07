package domain

import (
	"testing"
	"time"
)

func TestPaymentIsPayableRequiresOpenStateAndUnexpiredTime(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	for _, test := range []struct {
		name    string
		payment Payment
		want    bool
	}{
		{name: "created", payment: Payment{Status: StatusCreated}, want: true},
		{name: "pending before expiry", payment: Payment{Status: StatusPending, ExpiresAt: &future}, want: true},
		{name: "expired", payment: Payment{Status: StatusPending, ExpiresAt: &past}},
		{name: "paid", payment: Payment{Status: StatusPaid, ExpiresAt: &future}},
		{name: "refunding", payment: Payment{Status: StatusRefunding, ExpiresAt: &future}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.payment.IsPayable(now); got != test.want {
				t.Fatalf("IsPayable=%v want %v", got, test.want)
			}
		})
	}
}

func TestPaymentTransitions(t *testing.T) {
	tests := []struct {
		from, to PaymentStatus
		allowed  bool
	}{
		{StatusCreated, StatusPending, true}, {StatusPending, StatusPaid, true}, {StatusPending, StatusClosed, true},
		{StatusPaid, StatusRefunding, true}, {StatusRefunding, StatusPartiallyRefunded, true}, {StatusPartiallyRefunded, StatusRefunded, true},
		{StatusClosed, StatusPaid, false}, {StatusFailed, StatusPaid, false}, {StatusRefunded, StatusPaid, false},
	}
	for _, test := range tests {
		if got := CanTransition(test.from, test.to); got != test.allowed {
			t.Fatalf("transition %s -> %s: got %v want %v", test.from, test.to, got, test.allowed)
		}
	}
}

func TestValidatePaymentInput(t *testing.T) {
	if err := ValidatePaymentInput("order-1", MethodStaticAlipay, 1, "CNY", "subject"); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for _, test := range []struct {
		name              string
		order             string
		method            PaymentMethod
		amount            int64
		currency, subject string
	}{
		{"amount", "order", MethodStaticAlipay, 0, "CNY", "subject"}, {"currency", "order", MethodStaticAlipay, 1, "USD", "subject"}, {"method", "order", "unknown", 1, "CNY", "subject"}, {"subject", "order", MethodStaticAlipay, 1, "CNY", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidatePaymentInput(test.order, test.method, test.amount, test.currency, test.subject); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
