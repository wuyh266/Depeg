package httpserver_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/persistence/mysql"
	"depeg.local/platform/internal/adapter/security/password"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
)

type registrationMailbox struct {
	codes  map[string]string
	resets chan resetEmail
	err    error
}

type resetEmail struct{ address, code string }

func (m *registrationMailbox) SendRegistrationCode(_ context.Context, email, code string) error {
	if m.err != nil {
		return m.err
	}
	m.codes[email] = code
	return nil
}

func (m *registrationMailbox) SendPasswordResetCode(_ context.Context, email, code string) error {
	m.resets <- resetEmail{email, code}
	return nil
}

func sendRegistrationCode(t *testing.T, f *fixture, email string) string {
	t.Helper()
	response := request(t, f.handler, "POST", "/api/v1/auth/register/email-code", "", map[string]string{"email": email})
	status(t, response, http.StatusNoContent)
	code := f.mailer.codes[email]
	if len(code) != 6 {
		t.Fatal("the mailbox did not receive a six-digit code")
	}
	return code
}

func registrationBody(credentials map[string]string, email, code string) map[string]string {
	return map[string]string{"username": credentials["username"], "password": credentials["password"], "email": email, "email_code": code}
}

func registerAccount(t *testing.T, f *fixture, credentials map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	email := strings.ToLower(credentials["username"]) + "@example.test"
	code := sendRegistrationCode(t, f, email)
	return request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, email, code))
}

func TestRegistrationRequiresVerifiedEmail(t *testing.T) {
	f := setup(t)
	credentials := map[string]string{"username": "researcher", "password": "correct-password"}
	email := "researcher@example.test"
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", credentials), http.StatusBadRequest)
	code := sendRegistrationCode(t, f, email)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, "other@example.test", code)), http.StatusBadRequest)

	db, _ := f.database.Session(context.Background())
	var storedHash string
	if err := db.Table("auth_registration_codes").Select("code_hash").Where("email = ?", email).Take(&storedHash).Error; err != nil {
		t.Fatal(err)
	}
	if len(storedHash) != 64 || storedHash == code {
		t.Fatal("the verification record must contain a keyed digest")
	}
	r := request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, " researcher@EXAMPLE.TEST ", code))
	status(t, r, http.StatusCreated)
	if strings.Contains(r.Body.String(), email) || strings.Contains(r.Body.String(), "email_code") {
		t.Fatal("registration returned private verification data")
	}
	var user struct {
		Email           string
		EmailVerifiedAt *time.Time
	}
	if err := db.Table("users").Where("username = ?", "researcher").Take(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Email != email || user.EmailVerifiedAt == nil {
		t.Fatal("the account lacks the verified email")
	}
	credentials["username"] = "other"
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, email, code)), http.StatusBadRequest)

	if err := db.Table("auth_registration_codes").Where("email = ?", email).Update("resend_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	code = sendRegistrationCode(t, f, email)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, email, code)), http.StatusConflict)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/login", "", credentials), http.StatusUnauthorized)
}

func TestRegistrationCodeLimitsAndExpiration(t *testing.T) {
	f := setup(t)
	email := "limit@example.test"
	code := sendRegistrationCode(t, f, email)
	r := request(t, f.handler, "POST", "/api/v1/auth/register/email-code", "", map[string]string{"email": "limit@EXAMPLE.TEST"})
	status(t, r, http.StatusTooManyRequests)
	if r.Header().Get("Retry-After") != "60" {
		t.Fatal("missing resend delay")
	}
	credentials := map[string]string{"username": "researcher", "password": "correct-password"}
	wrong := "123456"
	if wrong == code {
		wrong = "654321"
	}
	for i := 0; i < auth.EmailCodeMaxAttempts; i++ {
		status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, email, wrong)), http.StatusBadRequest)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, email, code)), http.StatusBadRequest)

	expiredEmail := "expired@example.test"
	expiredCode := sendRegistrationCode(t, f, expiredEmail)
	db, _ := f.database.Session(context.Background())
	if err := db.Table("auth_registration_codes").Where("email = ?", expiredEmail).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", registrationBody(credentials, expiredEmail, expiredCode)), http.StatusBadRequest)
	for i := 2; i < auth.EmailSendLimit; i++ {
		sendRegistrationCode(t, f, fmt.Sprintf("account%d@example.test", i))
	}
	r = request(t, f.handler, "POST", "/api/v1/auth/register/email-code", "", map[string]string{"email": "overflow@example.test"})
	status(t, r, http.StatusTooManyRequests)
	if r.Header().Get("Retry-After") != "600" {
		t.Fatal("missing source rate limit delay")
	}
}

func TestRegistrationCodeSurvivesFailedAccountWrite(t *testing.T) {
	f := setup(t)
	email := "retry@example.test"
	code := sendRegistrationCode(t, f, email)
	body := registrationBody(map[string]string{"username": "researcher", "password": "correct-password"}, email, code)
	db, _ := f.database.Session(context.Background())
	if err := db.Exec(`CREATE TRIGGER reject_registration BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'temporary failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", body), http.StatusServiceUnavailable)
	if err := db.Exec("DROP TRIGGER reject_registration").Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", body), http.StatusCreated)
}

func TestExistingAccountLoginDuringMailFailure(t *testing.T) {
	f := setup(t)
	f.mailer.err = domain.ErrUnavailable
	ctx := context.Background()
	hash, err := password.New().Hash(ctx, "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := mysql.NewUserRepository(f.database).Create(ctx, auth.User{ID: "existing-user", Username: "existing", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register/email-code", "", map[string]string{"email": "delivery@example.test"}), http.StatusServiceUnavailable)
	credentials := map[string]string{"username": "existing", "password": "correct-password"}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/login", "", credentials), http.StatusOK)
}
