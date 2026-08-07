package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/atlaspay/meridian-gateway/internal/domain"
	"github.com/atlaspay/meridian-gateway/internal/security"
)

func (s *Store) CreateMerchant(ctx context.Context, name string, feeBPS int) (domain.Merchant, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return domain.Merchant{}, fmt.Errorf("merchant name must contain 1-128 characters")
	}
	if feeBPS < 0 || feeBPS > 10000 {
		return domain.Merchant{}, fmt.Errorf("fee_bps must be between 0 and 10000")
	}
	id, err := security.NewID("mch")
	if err != nil {
		return domain.Merchant{}, err
	}
	created := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO merchants(id,name,fee_bps,created_at) VALUES(?,?,?,?)`, id, name, feeBPS, created); err != nil {
		return domain.Merchant{}, fmt.Errorf("create merchant: %w", err)
	}
	return domain.Merchant{ID: id, Name: name, Status: "active", FeeBPS: feeBPS, CreatedAt: parseTime(sql.NullString{String: created, Valid: true})}, nil
}

func (s *Store) ListMerchants(ctx context.Context) ([]domain.Merchant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,status,fee_bps,created_at FROM merchants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Merchant
	for rows.Next() {
		var item domain.Merchant
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.FeeBPS, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetMerchant(ctx context.Context, id string) (domain.Merchant, error) {
	var item domain.Merchant
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,status,fee_bps,created_at FROM merchants WHERE id=?`, id).Scan(&item.ID, &item.Name, &item.Status, &item.FeeBPS, &created)
	if errorsIsNoRows(err) {
		return domain.Merchant{}, ErrNotFound
	}
	if err != nil {
		return domain.Merchant{}, err
	}
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, nil
}

func (s *Store) GetMerchantByAdminToken(ctx context.Context, token string) (domain.Merchant, error) {
	var item domain.Merchant
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,status,fee_bps,created_at FROM merchants WHERE admin_token_hash=? AND status='active'`, security.Digest(token)).Scan(&item.ID, &item.Name, &item.Status, &item.FeeBPS, &created)
	if errorsIsNoRows(err) {
		return domain.Merchant{}, ErrNotFound
	}
	if err != nil {
		return domain.Merchant{}, err
	}
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, nil
}

func (s *Store) RotateMerchantAdminToken(ctx context.Context, merchantID, actor string) (string, error) {
	if _, err := s.GetMerchant(ctx, merchantID); err != nil {
		return "", err
	}
	token, err := security.NewSecret("mmt")
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE merchants SET admin_token_hash=? WHERE id=?`, security.Digest(token), merchantID)
	if err != nil {
		return "", err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return "", ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "merchant.admin_token_rotated", "merchant", merchantID, "{}", now()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) CreateApplication(ctx context.Context, merchantID, name, webhookURL string) (domain.Application, string, error) {
	if _, err := s.GetMerchant(ctx, merchantID); err != nil {
		return domain.Application{}, "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return domain.Application{}, "", fmt.Errorf("application name must contain 1-128 characters")
	}
	if len(webhookURL) > 2048 {
		return domain.Application{}, "", fmt.Errorf("webhook_url is too long")
	}
	id, err := security.NewID("app")
	if err != nil {
		return domain.Application{}, "", err
	}
	apiKey, err := security.NewSecret("mk")
	if err != nil {
		return domain.Application{}, "", err
	}
	webhookSecret, err := security.NewSecret("whsec")
	if err != nil {
		return domain.Application{}, "", err
	}
	ciphertext, err := s.enc.Seal([]byte(webhookSecret), "application:"+id+":webhook_secret")
	if err != nil {
		return domain.Application{}, "", err
	}
	created := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO applications(id,merchant_id,name,api_key_hash,webhook_url,webhook_secret_cipher,created_at) VALUES(?,?,?,?,?,?,?)`, id, merchantID, name, security.Digest(apiKey), webhookURL, ciphertext, created); err != nil {
		return domain.Application{}, "", fmt.Errorf("create application: %w", err)
	}
	return domain.Application{ID: id, MerchantID: merchantID, Name: name, WebhookURL: webhookURL, WebhookSecret: webhookSecret, Status: "active", CreatedAt: parseTime(sql.NullString{String: created, Valid: true})}, apiKey, nil
}

func (s *Store) ListApplications(ctx context.Context, merchantID string) ([]domain.Application, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,merchant_id,name,webhook_url,status,created_at FROM applications WHERE merchant_id=? ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Application
	for rows.Next() {
		var item domain.Application
		var created string
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.Name, &item.WebhookURL, &item.Status, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetApplication(ctx context.Context, merchantID, id string) (domain.Application, error) {
	var item domain.Application
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,name,webhook_url,status,created_at FROM applications WHERE merchant_id=? AND id=?`, merchantID, id).Scan(&item.ID, &item.MerchantID, &item.Name, &item.WebhookURL, &item.Status, &created)
	if errorsIsNoRows(err) {
		return domain.Application{}, ErrNotFound
	}
	if err != nil {
		return domain.Application{}, err
	}
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, nil
}

func (s *Store) GetApplicationByAPIKey(ctx context.Context, apiKey string) (domain.Application, error) {
	var item domain.Application
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,name,webhook_url,status,created_at FROM applications WHERE api_key_hash=? AND status='active'`, security.Digest(apiKey)).Scan(&item.ID, &item.MerchantID, &item.Name, &item.WebhookURL, &item.Status, &created)
	if errorsIsNoRows(err) {
		return domain.Application{}, ErrNotFound
	}
	if err != nil {
		return domain.Application{}, err
	}
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, nil
}

func (s *Store) UpdateApplication(ctx context.Context, merchantID, id, name, webhookURL, status, actor string) (domain.Application, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return domain.Application{}, fmt.Errorf("application name must contain 1-128 characters")
	}
	if len(webhookURL) > 2048 {
		return domain.Application{}, fmt.Errorf("webhook_url is too long")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Application{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE applications SET name=?,webhook_url=?,status=? WHERE merchant_id=? AND id=?`, name, webhookURL, status, merchantID, id)
	if err != nil {
		return domain.Application{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return domain.Application{}, ErrNotFound
	}
	details, _ := json.Marshal(map[string]any{"status": status, "webhook_configured": strings.TrimSpace(webhookURL) != ""})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "application.updated", "application", id, details, now()); err != nil {
		return domain.Application{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Application{}, err
	}
	return s.GetApplication(ctx, merchantID, id)
}

func (s *Store) RotateApplicationCredentials(ctx context.Context, merchantID, id, actor string) (domain.Application, string, error) {
	item, err := s.GetApplication(ctx, merchantID, id)
	if err != nil {
		return domain.Application{}, "", err
	}
	apiKey, err := security.NewSecret("mk")
	if err != nil {
		return domain.Application{}, "", err
	}
	webhookSecret, err := security.NewSecret("whsec")
	if err != nil {
		return domain.Application{}, "", err
	}
	ciphertext, err := s.enc.Seal([]byte(webhookSecret), "application:"+id+":webhook_secret")
	if err != nil {
		return domain.Application{}, "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Application{}, "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE applications SET api_key_hash=?,webhook_secret_cipher=? WHERE merchant_id=? AND id=?`, security.Digest(apiKey), ciphertext, merchantID, id)
	if err != nil {
		return domain.Application{}, "", err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return domain.Application{}, "", ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, actor, "application.credentials_rotated", "application", id, "{}", now()); err != nil {
		return domain.Application{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return domain.Application{}, "", err
	}
	item.WebhookSecret = webhookSecret
	return item, apiKey, nil
}

func (s *Store) getApplicationWithSecret(ctx context.Context, id string) (domain.Application, error) {
	var item domain.Application
	var cipherText, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,name,webhook_url,webhook_secret_cipher,status,created_at FROM applications WHERE id=?`, id).Scan(&item.ID, &item.MerchantID, &item.Name, &item.WebhookURL, &cipherText, &item.Status, &created)
	if errorsIsNoRows(err) {
		return domain.Application{}, ErrNotFound
	}
	if err != nil {
		return domain.Application{}, err
	}
	secret, err := s.decrypt("application:"+id+":webhook_secret", cipherText)
	if err != nil {
		return domain.Application{}, err
	}
	item.WebhookSecret = secret
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, nil
}

func (s *Store) PutAlipayAccount(ctx context.Context, merchantID, gatewayURL, notifyURL string, credentials domain.AlipayCredentials) (domain.AlipayAccount, error) {
	if _, err := s.GetMerchant(ctx, merchantID); err != nil {
		return domain.AlipayAccount{}, err
	}
	if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.PrivateKeyPEM) == "" || strings.TrimSpace(credentials.AlipayPublicKeyPEM) == "" {
		return domain.AlipayAccount{}, fmt.Errorf("app_id, private_key_pem and alipay_public_key_pem are required")
	}
	if gatewayURL == "" {
		gatewayURL = "https://openapi.alipay.com/gateway.do"
	}
	if !strings.HasPrefix(gatewayURL, "https://") && !strings.HasPrefix(gatewayURL, "http://127.0.0.1") && !strings.HasPrefix(gatewayURL, "http://localhost") {
		return domain.AlipayAccount{}, fmt.Errorf("gateway_url must use HTTPS except localhost")
	}
	id, err := security.NewID("chn")
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	ciphertext, err := s.enc.Seal(encoded, "channel-account:"+merchantID+":alipay")
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	created := now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AlipayAccount{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO channel_accounts(id,merchant_id,channel,app_id,credentials_cipher,gateway_url,notify_url,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(merchant_id,channel) DO UPDATE SET app_id=excluded.app_id, credentials_cipher=excluded.credentials_cipher, gateway_url=excluded.gateway_url, notify_url=excluded.notify_url, updated_at=excluded.updated_at`,
		id, merchantID, "alipay", credentials.AppID, ciphertext, gatewayURL, notifyURL, created, created)
	if err != nil {
		return domain.AlipayAccount{}, fmt.Errorf("save alipay account: %w", err)
	}
	var accountID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM channel_accounts WHERE merchant_id=? AND channel='alipay'`, merchantID).Scan(&accountID); err != nil {
		return domain.AlipayAccount{}, err
	}
	details, _ := json.Marshal(map[string]any{"app_id": credentials.AppID, "gateway_url": gatewayURL})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,merchant_id,actor,action,resource_type,resource_id,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, mustID("aud"), merchantID, "admin", "channel_account.updated", "channel_account", accountID, details, created); err != nil {
		return domain.AlipayAccount{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AlipayAccount{}, err
	}
	item, _, err := s.GetAlipayAccount(ctx, merchantID)
	return item, err
}

func (s *Store) GetAlipayAccount(ctx context.Context, merchantID string) (domain.AlipayAccount, domain.AlipayCredentials, error) {
	var item domain.AlipayAccount
	var cipherText, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,app_id,credentials_cipher,gateway_url,notify_url,status,created_at,updated_at FROM channel_accounts WHERE merchant_id=? AND channel='alipay' AND status='active'`, merchantID).Scan(&item.ID, &item.MerchantID, &item.AppID, &cipherText, &item.GatewayURL, &item.NotifyURL, &item.Status, &created, &updated)
	if errorsIsNoRows(err) {
		return domain.AlipayAccount{}, domain.AlipayCredentials{}, ErrNotFound
	}
	if err != nil {
		return domain.AlipayAccount{}, domain.AlipayCredentials{}, err
	}
	plain, err := s.enc.Open(cipherText, "channel-account:"+merchantID+":alipay")
	if err != nil {
		return domain.AlipayAccount{}, domain.AlipayCredentials{}, err
	}
	var credentials domain.AlipayCredentials
	if err := json.Unmarshal(plain, &credentials); err != nil {
		return domain.AlipayAccount{}, domain.AlipayCredentials{}, err
	}
	item.HasPrivateKey = credentials.PrivateKeyPEM != ""
	item.HasAlipayPublicKey = credentials.AlipayPublicKeyPEM != ""
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	item.UpdatedAt = parseTime(sql.NullString{String: updated, Valid: true})
	return item, credentials, nil
}

func (s *Store) CreateStaticCode(ctx context.Context, merchantID, name, storeName, currency, mimeType string, image []byte) (domain.StaticCode, error) {
	if _, err := s.GetMerchant(ctx, merchantID); err != nil {
		return domain.StaticCode{}, err
	}
	if len(image) == 0 || len(image) > 5*1024*1024 {
		return domain.StaticCode{}, fmt.Errorf("static code image must be between 1 byte and 5 MiB")
	}
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		return domain.StaticCode{}, fmt.Errorf("currency must be CNY")
	}
	id, err := security.NewID("qr")
	if err != nil {
		return domain.StaticCode{}, err
	}
	created := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO static_codes(id,merchant_id,name,store_name,currency,mime_type,image,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, merchantID, strings.TrimSpace(name), strings.TrimSpace(storeName), currency, mimeType, image, created); err != nil {
		return domain.StaticCode{}, fmt.Errorf("create static code: %w", err)
	}
	return domain.StaticCode{ID: id, MerchantID: merchantID, Name: name, StoreName: storeName, Currency: currency, MIMEType: mimeType, Enabled: true, CreatedAt: parseTime(sql.NullString{String: created, Valid: true})}, nil
}

func (s *Store) ListStaticCodes(ctx context.Context, merchantID string) ([]domain.StaticCode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,merchant_id,name,store_name,currency,mime_type,enabled,created_at FROM static_codes WHERE merchant_id=? ORDER BY created_at DESC`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.StaticCode
	for rows.Next() {
		var item domain.StaticCode
		var enabled int
		var created string
		if err := rows.Scan(&item.ID, &item.MerchantID, &item.Name, &item.StoreName, &item.Currency, &item.MIMEType, &enabled, &created); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetStaticCode(ctx context.Context, merchantID, id string) (domain.StaticCode, []byte, error) {
	var item domain.StaticCode
	var enabled int
	var created string
	var image []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,merchant_id,name,store_name,currency,mime_type,image,enabled,created_at FROM static_codes WHERE merchant_id=? AND id=?`, merchantID, id).Scan(&item.ID, &item.MerchantID, &item.Name, &item.StoreName, &item.Currency, &item.MIMEType, &image, &enabled, &created)
	if errorsIsNoRows(err) {
		return domain.StaticCode{}, nil, ErrNotFound
	}
	if err != nil {
		return domain.StaticCode{}, nil, err
	}
	item.Enabled = enabled == 1
	item.CreatedAt = parseTime(sql.NullString{String: created, Valid: true})
	return item, image, nil
}

func (s *Store) SetStaticCodeEnabled(ctx context.Context, merchantID, id string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE static_codes SET enabled=? WHERE merchant_id=? AND id=?`, value, merchantID, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func errorsIsNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
