package config

import "testing"

func TestSMTPModeAndValidation(t *testing.T) {
	for _, test := range []struct {
		address, mode, want string
		valid               bool
	}{
		{"smtp.example.test:465", "auto", "tls", true},
		{"smtp.example.test:587", "auto", "starttls", true},
		{"smtp.example.test:2525", "", "starttls", true},
		{"smtp.example.test:465", "starttls", "", false},
		{"smtp.example.test:587", "tls", "", false},
		{"smtp.example.test:25", "none", "", false},
		{"127.0.0.1:2525", "none", "none", true},
	} {
		t.Run(test.address+test.mode, func(t *testing.T) {
			s := SMTP{Address: test.address, TLSMode: test.mode, From: "sender@example.test"}
			err := s.validate()
			if (err == nil) != test.valid {
				t.Fatalf("unexpected validation: %v", err)
			}
			if test.valid && s.ResolvedTLSMode() != test.want {
				t.Fatal("incorrect TLS mode")
			}
		})
	}
}
