package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB uint32 = 64 * 1024
	argonTime      uint32 = 3
	argonThreads   uint8  = 2
	argonKeyLen    uint32 = 32
	saltLenBytes          = 16
)

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password is required")
	}

	salt := make([]byte, saltLenBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemoryKiB, argonTime, argonThreads, saltB64, hashB64), nil
}

func VerifyPassword(encodedHash, password string) (bool, error) {
	params, salt, expectedHash, err := parseEncodedHash(encodedHash)
	if err != nil {
		return false, err
	}

	computed := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expectedHash)))
	if subtle.ConstantTimeCompare(expectedHash, computed) == 1 {
		return true, nil
	}
	return false, nil
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parseEncodedHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return argonParams{}, nil, nil, errors.New("invalid password hash format")
	}
	if parts[0] != "argon2id" {
		return argonParams{}, nil, nil, errors.New("unsupported password hash algorithm")
	}
	if parts[1] != "v=19" {
		return argonParams{}, nil, nil, errors.New("unsupported password hash version")
	}

	params := argonParams{}
	values := strings.Split(parts[2], ",")
	if len(values) != 3 {
		return argonParams{}, nil, nil, errors.New("invalid password hash params")
	}

	for _, value := range values {
		key, raw, ok := strings.Cut(value, "=")
		if !ok {
			return argonParams{}, nil, nil, errors.New("invalid password hash params")
		}
		switch key {
		case "m":
			parsed, err := strconv.ParseUint(raw, 10, 32)
			if err != nil {
				return argonParams{}, nil, nil, errors.New("invalid memory param")
			}
			params.memory = uint32(parsed)
		case "t":
			parsed, err := strconv.ParseUint(raw, 10, 32)
			if err != nil {
				return argonParams{}, nil, nil, errors.New("invalid time param")
			}
			params.time = uint32(parsed)
		case "p":
			parsed, err := strconv.ParseUint(raw, 10, 8)
			if err != nil {
				return argonParams{}, nil, nil, errors.New("invalid threads param")
			}
			params.threads = uint8(parsed)
		default:
			return argonParams{}, nil, nil, errors.New("unknown password hash param")
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return argonParams{}, nil, nil, errors.New("invalid salt encoding")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, errors.New("invalid hash encoding")
	}

	return params, salt, hash, nil
}
