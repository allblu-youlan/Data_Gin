package reportsecret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var ErrInvalidCredential = errors.New("invalid report credential")

const credentialPrefix = "v1:"

type Keyring struct {
	keys map[string][]byte
}

type EnvironmentKeyring struct {
	Variable        string
	VersionVariable string
}

// Validate verifies that the active credential key version is backed by a
// configured AES-256 key without encrypting or exposing any credential.
func (environment EnvironmentKeyring) Validate() error {
	versionVariable := strings.TrimSpace(environment.VersionVariable)
	if versionVariable == "" {
		versionVariable = "REPORT_CREDENTIAL_KEY_VERSION"
	}
	version := strings.TrimSpace(os.Getenv(versionVariable))
	if version == "" {
		return credentialError("credential key version is unavailable")
	}
	variable := strings.TrimSpace(environment.Variable)
	if variable == "" {
		variable = "REPORT_CREDENTIAL_KEYS_JSON"
	}
	keyring, err := ParseKeyring(os.Getenv(variable))
	if err != nil {
		return fmt.Errorf("load report credential keyring: %w", err)
	}
	if _, err := keyring.aead(version); err != nil {
		return err
	}
	return nil
}

func (environment EnvironmentKeyring) Encrypt(plaintext string) (string, string, error) {
	return environment.EncryptScoped("report-datasource", plaintext)
}

func (environment EnvironmentKeyring) EncryptScoped(purpose, plaintext string) (string, string, error) {
	variable := strings.TrimSpace(environment.Variable)
	if variable == "" {
		variable = "REPORT_CREDENTIAL_KEYS_JSON"
	}
	versionVariable := strings.TrimSpace(environment.VersionVariable)
	if versionVariable == "" {
		versionVariable = "REPORT_CREDENTIAL_KEY_VERSION"
	}
	version := strings.TrimSpace(os.Getenv(versionVariable))
	if version == "" {
		return "", "", credentialError("credential key version is unavailable")
	}
	keyring, err := ParseKeyring(os.Getenv(variable))
	if err != nil {
		return "", "", fmt.Errorf("load report credential keyring: %w", err)
	}
	ciphertext, err := keyring.EncryptScoped(version, purpose, plaintext)
	if err != nil {
		return "", "", err
	}
	return version, ciphertext, nil
}

func (environment EnvironmentKeyring) Decrypt(version, ciphertext string) (string, error) {
	return environment.DecryptScoped("report-datasource", version, ciphertext)
}

func (environment EnvironmentKeyring) DecryptScoped(purpose, version, ciphertext string) (string, error) {
	variable := strings.TrimSpace(environment.Variable)
	if variable == "" {
		variable = "REPORT_CREDENTIAL_KEYS_JSON"
	}
	keyring, err := ParseKeyring(os.Getenv(variable))
	if err != nil {
		return "", fmt.Errorf("load report credential keyring: %w", err)
	}
	return keyring.DecryptScoped(version, purpose, ciphertext)
}

func ParseKeyring(raw string) (*Keyring, error) {
	var encoded map[string]string
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encoded); err != nil || len(encoded) == 0 {
		return nil, credentialError("keyring must be a non-empty JSON object")
	}
	keys := make(map[string][]byte, len(encoded))
	for version, value := range encoded {
		version = strings.TrimSpace(version)
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if version == "" || err != nil || len(key) != 32 {
			return nil, credentialError("keyring contains an invalid AES-256 key")
		}
		keys[version] = append([]byte(nil), key...)
	}
	return &Keyring{keys: keys}, nil
}

func (keyring *Keyring) Encrypt(version, plaintext string) (string, error) {
	return keyring.EncryptScoped(version, "report-datasource", plaintext)
}

func (keyring *Keyring) EncryptScoped(version, purpose, plaintext string) (string, error) {
	aead, err := keyring.aead(version)
	if err != nil {
		return "", err
	}
	if plaintext == "" {
		return "", credentialError("plaintext is required")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encrypt report credential: generate nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), scopedAAD(purpose, version))
	return credentialPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (keyring *Keyring) Decrypt(version, ciphertext string) (string, error) {
	return keyring.DecryptScoped(version, "report-datasource", ciphertext)
}

func (keyring *Keyring) DecryptScoped(version, purpose, ciphertext string) (string, error) {
	aead, err := keyring.aead(version)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(ciphertext, credentialPrefix) {
		return "", credentialError("ciphertext format is invalid")
	}
	encoded := strings.TrimPrefix(ciphertext, credentialPrefix)
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) <= aead.NonceSize() {
		return "", credentialError("ciphertext format is invalid")
	}
	nonce, encrypted := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, encrypted, scopedAAD(purpose, version))
	if err != nil {
		return "", credentialError("ciphertext authentication failed")
	}
	if len(plaintext) == 0 {
		return "", credentialError("decrypted credential is empty")
	}
	return string(plaintext), nil
}

func (keyring *Keyring) aead(version string) (cipher.AEAD, error) {
	if keyring == nil || len(keyring.keys) == 0 {
		return nil, credentialError("keyring is unavailable")
	}
	version = strings.TrimSpace(version)
	key, exists := keyring.keys[version]
	if !exists || len(key) != 32 {
		return nil, credentialError("credential key version is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize report credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize report credential GCM: %w", err)
	}
	return aead, nil
}

func credentialAAD(version string) []byte {
	return scopedAAD("report-datasource", version)
}

func scopedAAD(purpose, version string) []byte {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = "invalid"
	}
	return []byte("Data_Gin/" + purpose + "/" + strings.TrimSpace(version))
}

func credentialError(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidCredential, message)
}
