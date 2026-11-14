package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/persistence/mysql"
	"depeg.local/platform/internal/adapter/security/password"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"golang.org/x/crypto/bcrypt"
)

const (
	requestPasswordResetURL = "/api/v1/auth/password-reset/email-code"
	resetPasswordURL        = "/api/v1/auth/password-reset"
)

func receiveResetCode(t *testing.T, f *fixture, email string) string {
	t.Helper()
	select {
	case message := <-f.mailer.resets:
		if message.address != email || len(message.code) != 6 {
			t.Fatal("unexpected reset email")
		}
		return message.code
	case <-time.After(3 * time.Second):
		t.Fatal("no password reset email")
		return ""
	}
}

func TestPasswordResetChangesCredentialsAndRevokesSessions(t *testing.T) {
	f := setup(t)
	first := login(t, f)
	otherCredentials := map[string]string{"username": "another-user", "password": "another-password"}
	status(t, registerAccount(t, f, otherCredentials), http.StatusCreated)
	other := pair(t, request(t, f.handler, http.MethodPost, "/api/v1/auth/login", "", otherCredentials))
	oldCredentials := auth.Credentials{Username: "researcher", Password: "correct-password"}
	second := pair(t, request(t, f.handler, http.MethodPost, "/api/v1/auth/login", "", oldCredentials))
	user, err := mysql.NewUserRepository(f.database).FindByUsername(context.Background(), oldCredentials.Username)
	if err != nil {
		t.Fatal(err)
	}
	email := "researcher@example.test"
	db, err := f.database.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("auth_registration_codes").Where("email = ?", email).Update("resend_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	registrationCode := sendRegistrationCode(t, f, email)
	status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", auth.PasswordReset{
		Email: email, EmailCode: registrationCode, Password: "must-not-be-used",
	}), http.StatusBadRequest)

	codeRequest := map[string]string{"email": email}
	status(t, request(t, f.handler, http.MethodPost, requestPasswordResetURL, "", codeRequest), http.StatusNoContent)
	code := receiveResetCode(t, f, email)
	status(t, request(t, f.handler, http.MethodPost, requestPasswordResetURL, "", codeRequest), http.StatusNoContent)
	select {
	case <-f.mailer.resets:
		t.Fatal("resend during cooldown sent a second code")
	default:
	}

	newPassword := "new-correct-password"
	body := auth.PasswordReset{Email: email, EmailCode: code, Password: newPassword}
	wrong := "123456"
	if wrong == code {
		wrong = "654321"
	}
	for _, invalid := range []auth.PasswordReset{
		{Email: email, EmailCode: wrong, Password: newPassword},
		{Email: "another@example.test", EmailCode: code, Password: newPassword},
	} {
		status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", invalid), http.StatusBadRequest)
	}
	status(t, request(t, f.handler, http.MethodGet, "/api/v1/auth/me", first.AccessToken, nil), http.StatusOK)

	status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", body), http.StatusNoContent)
	status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", body), http.StatusBadRequest)
	status(t, request(t, f.handler, http.MethodPost, "/api/v1/auth/login", "", oldCredentials), http.StatusUnauthorized)
	for _, tokens := range []auth.TokenPair{first, second} {
		status(t, request(t, f.handler, http.MethodGet, "/api/v1/auth/me", tokens.AccessToken, nil), http.StatusUnauthorized)
		status(t, request(t, f.handler, http.MethodPost, "/api/v1/auth/refresh", "", map[string]string{"refresh_token": tokens.RefreshToken}), http.StatusUnauthorized)
	}
	oldCredentials.Password = newPassword
	status(t, request(t, f.handler, http.MethodPost, "/api/v1/auth/login", "", oldCredentials), http.StatusOK)
	status(t, request(t, f.handler, http.MethodGet, "/api/v1/auth/me", other.AccessToken, nil), http.StatusOK)

	updated, err := mysql.NewUserRepository(f.database).FindByUsername(context.Background(), oldCredentials.Username)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(updated.PasswordHash))
	if err != nil || cost != bcrypt.DefaultCost || updated.PasswordHash == user.PasswordHash {
		t.Fatal("password reset did not store a fresh bcrypt hash")
	}

	// A login may have verified the previous hash right before the reset committed.
	stale := auth.Session{ID: "stale-login", UserID: user.ID, RefreshHash: "stale-login", ExpiresAt: time.Now().Add(time.Hour)}
	if err := mysql.NewSessionStore(f.database).Save(context.Background(), stale, user.PasswordHash); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("session with stale credentials: %v", err)
	}
}

func TestPasswordResetTreatsUnknownAndUnverifiedEmailsAlike(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	users := mysql.NewUserRepository(f.database)
	if err := users.Create(ctx, auth.User{ID: "unverified-user", Username: "unverified", Email: "unverified@example.test", PasswordHash: "unused"}); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"unknown@example.test", "unverified@example.test"} {
		status(t, request(t, f.handler, http.MethodPost, requestPasswordResetURL, "", map[string]string{"email": email}), http.StatusNoContent)
		status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", auth.PasswordReset{
			Email: email, EmailCode: "123456", Password: "valid-test-password",
		}), http.StatusBadRequest)
	}
	select {
	case <-f.mailer.resets:
		t.Fatal("sent a reset code to an unverified address")
	default:
	}
}

func TestPasswordResetRollsBackIfRevokingSessionsFails(t *testing.T) {
	f := setup(t)
	tokens := login(t, f)
	email := "researcher@example.test"
	status(t, request(t, f.handler, http.MethodPost, requestPasswordResetURL, "", map[string]string{"email": email}), http.StatusNoContent)
	body := auth.PasswordReset{Email: email, EmailCode: receiveResetCode(t, f, email), Password: "new-correct-password"}
	db, err := f.database.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_revocation BEFORE UPDATE OF revoked_at ON auth_sessions BEGIN SELECT RAISE(ABORT, 'temporary failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", body), http.StatusServiceUnavailable)
	status(t, request(t, f.handler, http.MethodGet, "/api/v1/auth/me", tokens.AccessToken, nil), http.StatusOK)
	user, err := mysql.NewUserRepository(f.database).FindByUsername(context.Background(), "researcher")
	if err != nil {
		t.Fatal(err)
	}
	if err := password.New().Compare(context.Background(), user.PasswordHash, "correct-password"); err != nil {
		t.Fatal("the failed transaction changed the password")
	}
	if err := db.Exec("DROP TRIGGER reject_revocation").Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, http.MethodPost, resetPasswordURL, "", body), http.StatusNoContent)
}
