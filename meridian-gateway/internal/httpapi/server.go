package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/alipay"
	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/gateway"
	"github.com/atlaspay/meridian-gateway/internal/security"
	qrcode "github.com/skip2/go-qrcode"
)

type Server struct {
	Gateway          *gateway.Service
	AdminToken       string
	BaseURL          string
	Environment      string
	AdminLocalOnly   bool
	Logger           *slog.Logger
	Assets           fs.FS
	OpenAPI          []byte
	AlipayRedirecter alipay.PayRedirecter
	startedAt        time.Time
}

type contextKey string

const (
	applicationKey contextKey = "application"
	adminAccessKey contextKey = "admin_access"
)

type adminAccess struct {
	Role       string `json:"role"`
	MerchantID string `json:"merchant_id,omitempty"`
}

func (a adminAccess) isSystem() bool { return a.Role == "system_admin" }
func (a adminAccess) allows(merchantID string) bool {
	return a.isSystem() || (a.Role == "merchant_admin" && a.MerchantID == merchantID)
}
func (a adminAccess) actor() string {
	if a.isSystem() {
		return "system_admin"
	}
	return "merchant_admin:" + a.MerchantID
}

func (s *Server) Handler() http.Handler {
	s.startedAt = time.Now().UTC()
	if s.AlipayRedirecter == nil {
		s.AlipayRedirecter = alipay.NewHTTPPayRedirecter(nil)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /openapi.json", s.openAPI)
	mux.HandleFunc("POST /v1/webhooks/alipay", s.alipayWebhook)
	mux.HandleFunc("GET /checkout/{token}", s.checkout)
	mux.HandleFunc("GET /checkout/{token}/status", s.checkoutStatus)
	mux.HandleFunc("GET /checkout/{token}/qr.png", s.checkoutQR)
	mux.HandleFunc("GET /checkout/{token}/static-code", s.checkoutStaticCode)
	mux.HandleFunc("GET /checkout/{token}/redirect", s.checkoutRedirect)
	mux.HandleFunc("POST /checkout/{token}/redirect", s.checkoutRedirectPost)
	mux.HandleFunc("GET /result", s.resultPage)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(s.Assets))))

	mux.Handle("GET /v1/payments/{id}", s.merchant(http.HandlerFunc(s.getPayment)))
	mux.Handle("POST /v1/payments", s.merchant(http.HandlerFunc(s.createPayment)))
	mux.Handle("POST /v1/payments/{id}/query", s.merchant(http.HandlerFunc(s.queryPayment)))
	mux.Handle("POST /v1/payments/{id}/close", s.merchant(http.HandlerFunc(s.closePayment)))
	mux.Handle("POST /v1/refunds", s.merchant(http.HandlerFunc(s.createRefund)))

	mux.Handle("GET /admin/api/session", s.admin(http.HandlerFunc(s.adminSession)))
	mux.Handle("GET /admin/api/merchants", s.admin(http.HandlerFunc(s.listMerchants)))
	mux.Handle("POST /admin/api/merchants", s.admin(http.HandlerFunc(s.createMerchant)))
	mux.Handle("POST /admin/api/merchants/{merchant}/access-token", s.admin(http.HandlerFunc(s.rotateMerchantAdminToken)))
	mux.Handle("GET /admin/api/merchants/{merchant}/applications", s.admin(http.HandlerFunc(s.listApplications)))
	mux.Handle("POST /admin/api/merchants/{merchant}/applications", s.admin(http.HandlerFunc(s.createApplication)))
	mux.Handle("PATCH /admin/api/merchants/{merchant}/applications/{id}", s.admin(http.HandlerFunc(s.updateApplication)))
	mux.Handle("POST /admin/api/merchants/{merchant}/applications/{id}/credentials", s.admin(http.HandlerFunc(s.rotateApplicationCredentials)))
	mux.Handle("PUT /admin/api/merchants/{merchant}/alipay", s.admin(http.HandlerFunc(s.putAlipayAccount)))
	mux.Handle("GET /admin/api/merchants/{merchant}/alipay", s.admin(http.HandlerFunc(s.getAlipayAccount)))
	mux.Handle("GET /admin/api/merchants/{merchant}/static-codes", s.admin(http.HandlerFunc(s.listStaticCodes)))
	mux.Handle("POST /admin/api/merchants/{merchant}/static-codes", s.admin(http.HandlerFunc(s.createStaticCode)))
	mux.Handle("PATCH /admin/api/merchants/{merchant}/static-codes/{id}", s.admin(http.HandlerFunc(s.updateStaticCode)))
	mux.Handle("GET /admin/api/merchants/{merchant}/payments", s.admin(http.HandlerFunc(s.listPayments)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments", s.admin(http.HandlerFunc(s.adminCreatePayment)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments/{id}/confirm", s.admin(http.HandlerFunc(s.confirmPayment)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments/{id}/abnormal", s.admin(http.HandlerFunc(s.abnormalPayment)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments/{id}/query", s.admin(http.HandlerFunc(s.adminQueryPayment)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments/{id}/close", s.admin(http.HandlerFunc(s.adminClosePayment)))
	mux.Handle("POST /admin/api/merchants/{merchant}/payments/{id}/refund", s.admin(http.HandlerFunc(s.adminRefundPayment)))
	mux.Handle("GET /admin/api/merchants/{merchant}/deliveries", s.admin(http.HandlerFunc(s.listDeliveries)))
	mux.Handle("POST /admin/api/merchants/{merchant}/deliveries/{id}/retry", s.admin(http.HandlerFunc(s.retryDelivery)))
	mux.Handle("GET /admin/api/merchants/{merchant}/audits", s.admin(http.HandlerFunc(s.listAudits)))
	mux.Handle("GET /admin/api/merchants/{merchant}/fees", s.admin(http.HandlerFunc(s.listFees)))
	mux.Handle("GET /admin/api/merchants/{merchant}/refunds", s.admin(http.HandlerFunc(s.listRefunds)))
	mux.Handle("GET /admin/api/merchants/{merchant}/reconciliation.csv", s.admin(http.HandlerFunc(s.reconciliation)))
	mux.HandleFunc("GET /admin", s.adminPage)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	})
	return s.securityHeaders(s.recover(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "uptime_seconds": int(time.Since(s.startedAt).Seconds())})
}
func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(s.OpenAPI)
}

func (s *Server) merchant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := strings.TrimSpace(r.Header.Get("Authorization"))
		apiKey, ok := strings.CutPrefix(value, "Bearer ")
		if !ok || apiKey == "" {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "Bearer API key is required")
			return
		}
		app, err := s.Gateway.Authenticate(r.Context(), apiKey)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key is invalid")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), applicationKey, app)))
	})
}

func (s *Server) admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AdminLocalOnly && !s.isLocal(r) {
			writeError(w, http.StatusForbidden, "local_only", "Admin API is only available from localhost")
			return
		}
		value := strings.TrimSpace(r.Header.Get("Authorization"))
		token, ok := strings.CutPrefix(value, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "invalid_admin_token", "Admin token is invalid")
			return
		}
		access := adminAccess{}
		if subtle.ConstantTimeCompare([]byte(security.Digest(token)), []byte(security.Digest(s.AdminToken))) == 1 {
			access.Role = "system_admin"
		} else {
			merchant, err := s.Gateway.AuthenticateMerchantAdmin(r.Context(), token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid_admin_token", "Admin token is invalid")
				return
			}
			access.Role = "merchant_admin"
			access.MerchantID = merchant.ID
		}
		if merchantID := r.PathValue("merchant"); merchantID != "" && !access.allows(merchantID) {
			writeError(w, http.StatusForbidden, "merchant_scope_denied", "Merchant access is denied")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminAccessKey, access)))
	})
}

func (s *Server) isLocal(r *http.Request) bool {
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) createPayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MerchantOrderID  string               `json:"merchant_order_id"`
		Method           domain.PaymentMethod `json:"method"`
		AmountMinor      int64                `json:"amount_minor"`
		Currency         string               `json:"currency"`
		Subject          string               `json:"subject"`
		StaticCodeID     string               `json:"static_code_id"`
		ReturnURL        string               `json:"return_url"`
		ExpiresInSeconds int64                `json:"expires_in_seconds"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	if body.Currency == "" {
		body.Currency = "CNY"
	}
	app := application(r)
	view, created, err := s.Gateway.CreatePayment(r.Context(), app, gateway.CreatePaymentInput{MerchantOrderID: body.MerchantOrderID, Method: body.Method, AmountMinor: body.AmountMinor, Currency: body.Currency, Subject: body.Subject, StaticCodeID: body.StaticCodeID, ReturnURL: body.ReturnURL, ExpiresIn: time.Duration(body.ExpiresInSeconds) * time.Second})
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, view)
}

func (s *Server) getPayment(w http.ResponseWriter, r *http.Request) {
	view, err := s.Gateway.GetPayment(r.Context(), application(r), r.PathValue("id"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) queryPayment(w http.ResponseWriter, r *http.Request) {
	view, err := s.Gateway.QueryPayment(r.Context(), application(r), r.PathValue("id"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) closePayment(w http.ResponseWriter, r *http.Request) {
	view, err := s.Gateway.ClosePayment(r.Context(), application(r), r.PathValue("id"), "api:"+application(r).ID)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) createRefund(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PaymentID        string `json:"payment_id"`
		MerchantRefundID string `json:"merchant_refund_id"`
		AmountMinor      int64  `json:"amount_minor"`
		Reason           string `json:"reason"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	refund, err := s.Gateway.Refund(r.Context(), application(r), body.PaymentID, body.MerchantRefundID, body.AmountMinor, body.Reason)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, refund)
}

func (s *Server) createMerchant(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	var body struct {
		Name   string `json:"name"`
		FeeBPS int    `json:"fee_bps"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	item, err := s.Gateway.CreateMerchant(r.Context(), body.Name, body.FeeBPS)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (s *Server) adminSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, adminAccessFrom(r))
}
func (s *Server) listMerchants(w http.ResponseWriter, r *http.Request) {
	access := adminAccessFrom(r)
	if !access.isSystem() {
		item, err := s.Gateway.Merchant(r.Context(), access.MerchantID)
		if err != nil {
			s.respondStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []domain.Merchant{item}})
		return
	}
	items, err := s.Gateway.ListMerchants(r.Context())
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) rotateMerchantAdminToken(w http.ResponseWriter, r *http.Request) {
	if !requireSystemAdmin(w, r) {
		return
	}
	token, err := s.Gateway.RotateMerchantAdminToken(r.Context(), r.PathValue("merchant"), adminAccessFrom(r).actor())
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"admin_token": token, "secret_notice": "Store this value now; it cannot be retrieved again."})
}
func (s *Server) createApplication(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		WebhookURL string `json:"webhook_url"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	item, apiKey, err := s.Gateway.CreateApplication(r.Context(), r.PathValue("merchant"), body.Name, body.WebhookURL)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"application": item, "api_key": apiKey, "webhook_secret": item.WebhookSecret, "secret_notice": "Store these values now; they cannot be retrieved again."})
}
func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.ListApplications(r.Context(), r.PathValue("merchant"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) updateApplication(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		WebhookURL string `json:"webhook_url"`
		Status     string `json:"status"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	item, err := s.Gateway.UpdateApplication(r.Context(), r.PathValue("merchant"), r.PathValue("id"), body.Name, body.WebhookURL, body.Status, adminAccessFrom(r).actor())
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) rotateApplicationCredentials(w http.ResponseWriter, r *http.Request) {
	item, apiKey, err := s.Gateway.RotateApplicationCredentials(r.Context(), r.PathValue("merchant"), r.PathValue("id"), adminAccessFrom(r).actor())
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"application": item, "api_key": apiKey, "webhook_secret": item.WebhookSecret, "secret_notice": "Store these values now; they cannot be retrieved again."})
}
func (s *Server) putAlipayAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Environment        domain.AlipayEnvironment `json:"environment"`
		AppID              string                   `json:"app_id"`
		PrivateKeyPEM      string                   `json:"private_key_pem"`
		AlipayPublicKeyPEM string                   `json:"alipay_public_key_pem"`
	}
	if !decodeJSON(w, r, &body, 2<<20) {
		return
	}
	item, err := s.Gateway.SaveAlipayAccount(r.Context(), r.PathValue("merchant"), body.Environment, gateway.AlipayCredentials{AppID: body.AppID, PrivateKeyPEM: body.PrivateKeyPEM, AlipayPublicKeyPEM: body.AlipayPublicKeyPEM})
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) getAlipayAccount(w http.ResponseWriter, r *http.Request) {
	item, err := s.Gateway.AlipayAccount(r.Context(), r.PathValue("merchant"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createStaticCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "Upload must be at most 5 MiB")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "image is required")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 5<<20+1))
	if err != nil || len(content) > 5<<20 {
		writeError(w, http.StatusBadRequest, "invalid_upload", "image is too large")
		return
	}
	mime := http.DetectContentType(content)
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		writeError(w, http.StatusBadRequest, "invalid_upload", "Only PNG, JPEG, or WebP is allowed")
		return
	}
	name := r.FormValue("name")
	if name == "" {
		name = header.Filename
	}
	item, err := s.Gateway.CreateStaticCode(r.Context(), r.PathValue("merchant"), name, r.FormValue("store_name"), "CNY", mime, content)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (s *Server) listStaticCodes(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.ListStaticCodes(r.Context(), r.PathValue("merchant"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) updateStaticCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	if err := s.Gateway.SetStaticCodeEnabled(r.Context(), r.PathValue("merchant"), r.PathValue("id"), body.Enabled); err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": body.Enabled})
}

func (s *Server) listPayments(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.AdminPayments(r.Context(), r.PathValue("merchant"), r.URL.Query().Get("status"), 100)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	for i := range items {
		items[i].CheckoutToken = ""
		items[i].QRContent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) adminCreatePayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ApplicationID    string               `json:"application_id"`
		MerchantOrderID  string               `json:"merchant_order_id"`
		Method           domain.PaymentMethod `json:"method"`
		AmountMinor      int64                `json:"amount_minor"`
		Currency         string               `json:"currency"`
		Subject          string               `json:"subject"`
		StaticCodeID     string               `json:"static_code_id"`
		ReturnURL        string               `json:"return_url"`
		ExpiresInSeconds int64                `json:"expires_in_seconds"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	if body.Currency == "" {
		body.Currency = "CNY"
	}
	view, created, err := s.Gateway.AdminCreatePayment(r.Context(), r.PathValue("merchant"), body.ApplicationID, gateway.CreatePaymentInput{MerchantOrderID: body.MerchantOrderID, Method: body.Method, AmountMinor: body.AmountMinor, Currency: body.Currency, Subject: body.Subject, StaticCodeID: body.StaticCodeID, ReturnURL: body.ReturnURL, ExpiresIn: time.Duration(body.ExpiresInSeconds) * time.Second})
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, view)
}
func (s *Server) confirmPayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note  string `json:"note"`
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	item, err := s.Gateway.ConfirmPayment(r.Context(), r.PathValue("merchant"), r.PathValue("id"), adminAccessFrom(r).actor(), body.Note)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) abnormalPayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note  string `json:"note"`
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	if err := s.Gateway.MarkAbnormal(r.Context(), r.PathValue("merchant"), r.PathValue("id"), adminAccessFrom(r).actor(), body.Note); err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": true})
}
func (s *Server) adminQueryPayment(w http.ResponseWriter, r *http.Request) {
	view, err := s.Gateway.AdminQueryPayment(r.Context(), r.PathValue("merchant"), r.PathValue("id"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) adminClosePayment(w http.ResponseWriter, r *http.Request) {
	view, err := s.Gateway.AdminClosePayment(r.Context(), r.PathValue("merchant"), r.PathValue("id"))
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
func (s *Server) adminRefundPayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MerchantRefundID string `json:"merchant_refund_id"`
		AmountMinor      int64  `json:"amount_minor"`
		Reason           string `json:"reason"`
	}
	if !decodeJSON(w, r, &body, 1<<20) {
		return
	}
	refund, err := s.Gateway.AdminRefund(r.Context(), r.PathValue("merchant"), r.PathValue("id"), body.MerchantRefundID, body.AmountMinor, body.Reason)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, refund)
}
func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.Deliveries(r.Context(), r.PathValue("merchant"), 100)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	for i := range items {
		items[i].Payload = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	if err := s.Gateway.RetryDelivery(r.Context(), r.PathValue("merchant"), r.PathValue("id"), adminAccessFrom(r).actor()); err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
}
func (s *Server) listAudits(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.Audits(r.Context(), r.PathValue("merchant"), 100)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) listFees(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.Fees(r.Context(), r.PathValue("merchant"), 500)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) listRefunds(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.Refunds(r.Context(), r.PathValue("merchant"), 500)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) alipayWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "failure", http.StatusBadRequest)
		return
	}
	if err := s.Gateway.HandleAlipayNotification(r.Context(), r.PostForm); err != nil {
		s.logError("alipay webhook", err)
		http.Error(w, "failure", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("success"))
}

func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	payable := payment.IsPayable(time.Now().UTC())
	var alipayForm *alipay.PayForm
	if payable && payment.RedirectURL != "" && (payment.Method == domain.MethodAlipayPage || payment.Method == domain.MethodAlipayWAP) {
		form, parseErr := alipay.ParsePayURL(payment.RedirectURL)
		if parseErr != nil {
			s.logError("parse Alipay checkout form", parseErr)
		} else {
			alipayForm = &form
		}
	}
	checkoutPath := "/checkout/" + url.PathEscape(payment.CheckoutToken)
	data := map[string]any{"payment": payment, "payable": payable, "status_url": checkoutPath + "/status", "static_code_url": checkoutPath + "/static-code", "qr_url": checkoutPath + "/qr.png", "redirect_url": checkoutPath + "/redirect", "alipay_form": alipayForm}
	s.renderPage(w, "checkout.html", data)
}
func (s *Server) checkoutStatus(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Payment not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": payment.ID, "status": payment.Status, "payable": payment.IsPayable(time.Now().UTC()), "paid_at": payment.PaidAt, "closed_at": payment.ClosedAt, "refunded_minor": payment.RefundedMinor})
}
func (s *Server) checkoutStaticCode(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil || payment.Method != domain.MethodStaticAlipay {
		http.NotFound(w, r)
		return
	}
	if !payment.IsPayable(time.Now().UTC()) {
		http.Error(w, "Payment is no longer payable", http.StatusGone)
		return
	}
	code, image, err := s.Gateway.StaticCodeImage(r.Context(), payment.MerchantID, payment.StaticCodeID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", code.MIMEType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Write(image)
}
func (s *Server) checkoutQR(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil || payment.QRContent == "" {
		http.NotFound(w, r)
		return
	}
	if !payment.IsPayable(time.Now().UTC()) {
		http.Error(w, "Payment is no longer payable", http.StatusGone)
		return
	}
	image, err := renderQR(payment.QRContent)
	if err != nil {
		http.Error(w, "QR unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Write(image)
}
func (s *Server) checkoutRedirect(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil || (payment.Method != domain.MethodAlipayPage && payment.Method != domain.MethodAlipayWAP) || payment.RedirectURL == "" {
		http.NotFound(w, r)
		return
	}
	if !payment.IsPayable(time.Now().UTC()) {
		http.Error(w, "Payment is no longer payable", http.StatusConflict)
		return
	}
	form, err := alipay.ParsePayURL(payment.RedirectURL)
	if err != nil {
		s.logError("parse Alipay redirect", err)
		http.Error(w, "Payment redirect unavailable", http.StatusBadGateway)
		return
	}
	s.renderPage(w, "redirect.html", map[string]any{"form": form, "redirect_url": r.URL.Path})
}
func (s *Server) checkoutRedirectPost(w http.ResponseWriter, r *http.Request) {
	payment, err := s.Gateway.CheckoutPayment(r.Context(), r.PathValue("token"))
	if err != nil || (payment.Method != domain.MethodAlipayPage && payment.Method != domain.MethodAlipayWAP) || payment.RedirectURL == "" {
		http.NotFound(w, r)
		return
	}
	if !payment.IsPayable(time.Now().UTC()) {
		http.Error(w, "Payment is no longer payable", http.StatusConflict)
		return
	}
	form, err := alipay.ParsePayURL(payment.RedirectURL)
	if err != nil {
		s.logError("parse Alipay redirect", err)
		http.Error(w, "Payment redirect unavailable", http.StatusBadGateway)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil || !matchesPayForm(r.PostForm, form.Fields) {
		http.Error(w, "Invalid payment form", http.StatusBadRequest)
		return
	}
	location, err := s.AlipayRedirecter.ResolvePayRedirect(r.Context(), form, alipay.PayRedirectMetadata{
		UserAgent:      r.UserAgent(),
		AcceptLanguage: r.Header.Get("Accept-Language"),
	})
	if err != nil {
		s.logError("request Alipay redirect", err)
		http.Error(w, "Payment redirect unavailable", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, location.String(), http.StatusSeeOther)
}
func matchesPayForm(values url.Values, fields []alipay.PayFormField) bool {
	if len(values) != len(fields) {
		return false
	}
	for _, field := range fields {
		all, ok := values[field.Name]
		if !ok || len(all) != 1 || all[0] != field.Value {
			return false
		}
	}
	return true
}
func (s *Server) resultPage(w http.ResponseWriter, r *http.Request) {
	paymentID := strings.TrimSpace(r.URL.Query().Get("payment_id"))
	payment, err := s.Gateway.ResultPayment(r.Context(), paymentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderPage(w, "result.html", map[string]any{
		"payment":    payment,
		"status_url": "/checkout/" + url.PathEscape(payment.CheckoutToken) + "/status",
	})
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	if s.AdminLocalOnly && !s.isLocal(r) {
		http.Error(w, "Admin is only available from localhost", http.StatusForbidden)
		return
	}
	adminToken := ""
	if strings.EqualFold(strings.TrimSpace(s.Environment), "development") {
		adminToken = s.AdminToken
	}
	s.renderPage(w, "admin.html", map[string]any{"base_url": s.BaseURL, "admin_token": adminToken})
}

func (s *Server) renderPage(w http.ResponseWriter, name string, data any) {
	content, err := fs.ReadFile(s.Assets, name)
	if err != nil {
		s.logError("read page", err)
		http.Error(w, "Page unavailable", http.StatusInternalServerError)
		return
	}
	tmpl, err := template.New(name).Parse(string(content))
	if err != nil {
		s.logError("parse page", err)
		http.Error(w, "Page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		s.logError("render page", err)
	}
}

func renderQR(content string) ([]byte, error) { return qrcode.Encode(content, qrcode.Medium, 384) }

func (s *Server) reconciliation(w http.ResponseWriter, r *http.Request) {
	items, err := s.Gateway.AdminPayments(r.Context(), r.PathValue("merchant"), "", 500)
	if err != nil {
		s.respondStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=meridian-reconciliation.csv")
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	refunds, err := s.Gateway.Refunds(r.Context(), r.PathValue("merchant"), 1000)
	if err != nil {
		s.logError("load reconciliation refunds", err)
		return
	}
	_ = writer.Write([]string{"record_type", "record_id", "payment_id", "merchant_order_id", "merchant_refund_id", "method", "status", "amount_minor", "currency", "channel_reference", "created_at"})
	for _, item := range items {
		_ = writer.Write([]string{"payment", item.ID, item.ID, item.MerchantOrderID, "", string(item.Method), string(item.Status), strconv.FormatInt(item.AmountMinor, 10), item.Currency, item.ChannelOrderID, item.CreatedAt.Format(time.RFC3339)})
	}
	for _, item := range refunds {
		_ = writer.Write([]string{"refund", item.ID, item.PaymentID, "", item.MerchantRefund, "", item.Status, strconv.FormatInt(-item.AmountMinor, 10), "CNY", item.ChannelRefund, item.CreatedAt.Format(time.RFC3339)})
	}
	writer.Flush()
}

func application(r *http.Request) domain.Application {
	return r.Context().Value(applicationKey).(domain.Application)
}

func adminAccessFrom(r *http.Request) adminAccess {
	access, _ := r.Context().Value(adminAccessKey).(adminAccess)
	return access
}

func requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	if adminAccessFrom(r).isSystem() {
		return true
	}
	writeError(w, http.StatusForbidden, "system_admin_required", "System administrator access is required")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON object")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *Server) respondStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gateway.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Resource not found")
	case errors.Is(err, gateway.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, gateway.ErrInvalidState):
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logError("panic", fmt.Errorf("%v", recovered))
				writeError(w, http.StatusInternalServerError, "internal_error", "Unexpected server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://openapi.alipay.com https://*.alipay.com https://openapi-sandbox.dl.alipaydev.com https://*.alipaydev.com")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) logError(message string, err error) {
	if s.Logger != nil {
		s.Logger.Error(message, "error", err)
	}
}
