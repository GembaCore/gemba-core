package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2idParams carries the parameters encoded in the PHC string. Chosen to
// be fast enough for per-request bearer verification while still providing
// meaningful work for offline brute-force of a disclosed hash file.
//
// Tokens are 256-bit random so argon2's brute-force resistance is irrelevant
// in the normal case; argon2id is specified in gm-e5.2 and provides a safety
// margin if operators ever substitute a weaker secret.
type argon2idParams struct {
	memoryKiB uint32
	time      uint32
	threads   uint8
	keyLen    uint32
	saltLen   uint32
}

var defaultArgon2Params = argon2idParams{
	memoryKiB: 64 * 1024,
	time:      1,
	threads:   4,
	keyLen:    32,
	saltLen:   16,
}

// HashToken computes an argon2id PHC-formatted hash of token. The returned
// string contains parameters + salt + hash so Verify does not need any
// configuration state.
func HashToken(token string) (string, error) {
	return hashTokenWithParams(token, defaultArgon2Params)
}

func hashTokenWithParams(token string, p argon2idParams) (string, error) {
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2id salt: %w", err)
	}
	key := argon2.IDKey([]byte(token), salt, p.time, p.memoryKiB, p.threads, p.keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memoryKiB, p.time, p.threads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyHash checks token against a PHC-encoded argon2id hash using a
// constant-time comparison. It is not constant-time against argon2id's
// own parameters; the parameters themselves are not secret.
func VerifyHash(token, encoded string) (bool, error) {
	p, salt, key, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(token), salt, p.time, p.memoryKiB, p.threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

func parsePHC(encoded string) (argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// Empty leading part before first '$' produces an initial "", hence 6.
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2idParams{}, nil, nil, errors.New("argon2id: malformed hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2idParams{}, nil, nil, fmt.Errorf("argon2id: parse version: %w", err)
	}
	if version != argon2.Version {
		return argon2idParams{}, nil, nil, fmt.Errorf("argon2id: unsupported version %d", version)
	}
	var p argon2idParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&p.memoryKiB, &p.time, &p.threads); err != nil {
		return argon2idParams{}, nil, nil, fmt.Errorf("argon2id: parse params: %w", err)
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return argon2idParams{}, nil, nil, fmt.Errorf("argon2id: decode salt: %w", err)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return argon2idParams{}, nil, nil, fmt.Errorf("argon2id: decode key: %w", err)
	}
	p.saltLen = uint32(len(salt))
	p.keyLen = uint32(len(key))
	return p, salt, key, nil
}
