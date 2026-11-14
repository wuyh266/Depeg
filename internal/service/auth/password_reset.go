package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"depeg.local/platform/internal/domain"
	account "depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
)

func (s *service) passwordResetCodeHash(userID, email, code string) string {
	return hex.EncodeToString(keyedDigest(s.codeKey, "password-reset\x00"+userID+"\x00"+email+"\x00"+code))
}

func (s *service) RequestPasswordReset(ctx context.Context, rawEmail, source string) error {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	if s.mailer == nil {
		return domain.ErrUnavailable
	}

	// Cap outstanding delivery work per process before looking up the account.
	select {
	case s.resetMailSlots <- struct{}{}:
	default:
		return domain.ErrUnavailable
	}
	scheduled := false
	defer func() {
		if !scheduled {
			<-s.resetMailSlots
		}
	}()

	user, err := s.users.FindByVerifiedEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: user lookup", domain.ErrUnavailable)
	}
	code, err := newEmailCode()
	if err != nil {
		return err
	}
	// Reserve an entry for any syntactically valid address so the cooldown
	// and per-source limits behave the same for registered and unknown addresses.
	err = s.resetCodes.Reserve(ctx, email, s.passwordResetCodeHash(user.ID, email, code), s.emailSourceHash(source))
	if errors.Is(err, account.ErrEmailCodeCooldown) {
		return nil
	}
	if err != nil || user.ID == "" {
		return err
	}
	scheduled = true
	go func() {
		defer func() { <-s.resetMailSlots }()
		// The SMTP adapter applies a delivery timeout. The HTTP response does
		// not wait for SMTP or disclose whether an email was sent.
		_ = s.mailer.SendPasswordResetCode(context.WithoutCancel(ctx), user.Email, code)
	}()
	return nil
}

func (s *service) ResetPassword(ctx context.Context, input account.PasswordReset) error {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if len(input.Password) < 8 || len(input.Password) > 72 {
		return domain.ErrInvalid
	}
	if !emailCodePattern.MatchString(input.EmailCode) {
		return account.ErrInvalidEmailCode
	}
	user, err := s.users.FindByVerifiedEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: user lookup", domain.ErrUnavailable)
	}

	return s.resetCodes.Use(ctx, email, s.passwordResetCodeHash(user.ID, email, input.EmailCode), func(users port.UserRepository) error {
		if user.ID == "" {
			return account.ErrInvalidEmailCode
		}
		hash, err := s.passwords.Hash(ctx, input.Password)
		if err != nil {
			return err
		}
		return users.ResetPassword(ctx, user.ID, email, hash)
	})
}
