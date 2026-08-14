package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

func Encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	if len(key) != 32 {
		return nil, nil, errors.New("AES-GCM key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("create AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("create GCM: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, []byte("cyber-amber:bili-account:v1"))
	return ciphertext, nonce, nil
}
func Decrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-GCM key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("encrypted account nonce has unexpected size")
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte("cyber-amber:bili-account:v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt account credentials: %w", err)
	}
	return plaintext, nil
}
