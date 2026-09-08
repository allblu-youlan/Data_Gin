package reportsecret

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestKeyringEncryptDecryptAndAuthenticate(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyring, err := ParseKeyring(`{"key-v1":"` + key + `"}`)
	if err != nil {
		t.Fatalf("ParseKeyring() error = %v", err)
	}
	ciphertext, err := keyring.Encrypt("key-v1", "oracle-password")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if !strings.HasPrefix(ciphertext, credentialPrefix) || strings.Contains(ciphertext, "oracle-password") {
		t.Fatalf("ciphertext = %q", ciphertext)
	}
	plaintext, err := keyring.Decrypt("key-v1", ciphertext)
	if err != nil || plaintext != "oracle-password" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}

	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(ciphertext, credentialPrefix))
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	tampered := credentialPrefix + base64.RawStdEncoding.EncodeToString(sealed)
	if _, err := keyring.Decrypt("key-v1", tampered); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Decrypt(tampered) error = %v", err)
	}
	if _, err := keyring.Decrypt("key-v2", ciphertext); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Decrypt(wrong version) error = %v", err)
	}
}

func TestParseKeyringRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"key-v1":"short"}`, `[]`} {
		if _, err := ParseKeyring(raw); !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("ParseKeyring(%q) error = %v", raw, err)
		}
	}
}

func TestEnvironmentKeyringLoadsConfiguredKeys(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyring, err := ParseKeyring(`{"key-v1":"` + key + `"}`)
	if err != nil {
		t.Fatalf("ParseKeyring() error = %v", err)
	}
	ciphertext, err := keyring.Encrypt("key-v1", "password")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	t.Setenv("TEST_REPORT_KEYS", `{"key-v1":"`+key+`"}`)
	plaintext, err := (EnvironmentKeyring{Variable: "TEST_REPORT_KEYS"}).Decrypt("key-v1", ciphertext)
	if err != nil || plaintext != "password" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
}

func TestEnvironmentKeyringEncryptsWithConfiguredCurrentVersion(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("TEST_REPORT_KEYS", `{"key-v2":"`+key+`"}`)
	t.Setenv("TEST_REPORT_KEY_VERSION", "key-v2")
	environment := EnvironmentKeyring{Variable: "TEST_REPORT_KEYS", VersionVariable: "TEST_REPORT_KEY_VERSION"}
	version, ciphertext, err := environment.Encrypt("oracle-password")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := environment.Decrypt(version, ciphertext)
	if err != nil || version != "key-v2" || plaintext != "oracle-password" || strings.Contains(ciphertext, plaintext) {
		t.Fatalf("Encrypt()/Decrypt() = version %q, plaintext %q, ciphertext %q, err %v", version, plaintext, ciphertext, err)
	}
}

func TestEnvironmentKeyringScopesCredentialsByPurpose(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("TEST_REPORT_KEYS", `{"key-v2":"`+key+`"}`)
	t.Setenv("TEST_REPORT_KEY_VERSION", "key-v2")
	environment := EnvironmentKeyring{Variable: "TEST_REPORT_KEYS", VersionVariable: "TEST_REPORT_KEY_VERSION"}
	version, ciphertext, err := environment.EncryptScoped("office-webdav", "app-password")
	if err != nil {
		t.Fatalf("EncryptScoped() error = %v", err)
	}
	plaintext, err := environment.DecryptScoped("office-webdav", version, ciphertext)
	if err != nil || plaintext != "app-password" {
		t.Fatalf("DecryptScoped() = %q, %v", plaintext, err)
	}
	if _, err := environment.Decrypt(version, ciphertext); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Decrypt() cross-purpose error = %v, want ErrInvalidCredential", err)
	}
}

func TestEnvironmentKeyringValidateRejectsMissingCurrentVersion(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("TEST_REPORT_KEYS", `{"key-v1":"`+key+`"}`)
	t.Setenv("TEST_REPORT_KEY_VERSION", "key-v2")
	environment := EnvironmentKeyring{Variable: "TEST_REPORT_KEYS", VersionVariable: "TEST_REPORT_KEY_VERSION"}
	if err := environment.Validate(); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Validate() error = %v, want ErrInvalidCredential", err)
	}
}

func TestScopedCiphertextCannotCrossPurposeBoundary(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	keyring, err := ParseKeyring(`{"key-v1":"` + key + `"}`)
	if err != nil {
		t.Fatalf("ParseKeyring() error = %v", err)
	}
	ciphertext, err := keyring.EncryptScoped("key-v1", reportParameterPurpose, `{"secret":"value"}`)
	if err != nil {
		t.Fatalf("EncryptScoped() error = %v", err)
	}
	if _, err := keyring.Decrypt("key-v1", ciphertext); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("Decrypt() error = %v, want ErrInvalidCredential", err)
	}
	plaintext, err := keyring.DecryptScoped("key-v1", reportParameterPurpose, ciphertext)
	if err != nil || plaintext != `{"secret":"value"}` {
		t.Fatalf("DecryptScoped() = %q, %v", plaintext, err)
	}
}
