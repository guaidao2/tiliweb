package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordParams controls argon2id. Defaults follow OWASP guidance.
type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

var DefaultPasswordParams = PasswordParams{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLen:     16,
	KeyLen:      32,
}

func HashPassword(password string) (string, error) {
	return HashPasswordWithParams(password, DefaultPasswordParams)
}

func HashPasswordWithParams(password string, p PasswordParams) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password is empty")
	}
	if p.Memory == 0 || p.Iterations == 0 || p.Parallelism == 0 || p.SaltLen < 8 || p.KeyLen < 16 {
		return "", fmt.Errorf("invalid argon2 params")
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	params, salt, want, err := decodePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1, nil
}

func PasswordNeedsRehash(encoded string, want PasswordParams) bool {
	got, salt, hash, err := decodePasswordHash(encoded)
	if err != nil {
		return true
	}
	return got.Memory != want.Memory ||
		got.Iterations != want.Iterations ||
		got.Parallelism != want.Parallelism ||
		len(salt) != int(want.SaltLen) ||
		len(hash) != int(want.KeyLen)
}

func decodePasswordHash(encoded string) (PasswordParams, []byte, []byte, error) {
	var p PasswordParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, fmt.Errorf("invalid argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, fmt.Errorf("unsupported argon2 version")
	}
	var parallelism uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &parallelism); err != nil {
		return p, nil, nil, fmt.Errorf("invalid argon2 params")
	}
	if p.Memory == 0 || p.Iterations == 0 || parallelism == 0 || parallelism > 255 {
		return p, nil, nil, fmt.Errorf("invalid argon2 params")
	}
	p.Parallelism = uint8(parallelism)

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return p, nil, nil, fmt.Errorf("invalid argon2 salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) == 0 {
		return p, nil, nil, fmt.Errorf("invalid argon2 hash")
	}
	p.SaltLen = uint32(len(salt))
	p.KeyLen = uint32(len(hash))
	return p, salt, hash, nil
}
