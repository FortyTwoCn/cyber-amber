package security

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

func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", errors.New("管理员密码至少需要 12 个字符")
	}
	if len(password) > 1024 {
		return "", errors.New("管理员密码不能超过 1024 字节")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	memory, timeCost, threads := uint32(64*1024), uint32(3), uint8(2)
	hash := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, timeCost, threads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func VerifyPassword(encoded, password string) (bool, error) {
	if len(password) > 1024 {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("unsupported admin password hash")
	}
	var memory, timeCost uint64
	var threads uint64
	for _, part := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false, err
		}
		switch key {
		case "m":
			memory = parsed
		case "t":
			timeCost = parsed
		case "p":
			threads = parsed
		}
	}
	if memory < 8*1024 || memory > 1024*1024 || timeCost < 1 || timeCost > 10 || threads < 1 || threads > 16 {
		return false, errors.New("argon2 parameters outside safe range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	if len(expected) != 32 {
		return false, errors.New("invalid argon2 hash length")
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(timeCost), uint32(memory), uint8(threads), 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
