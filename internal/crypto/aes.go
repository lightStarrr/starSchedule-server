// Package crypto 提供上游 API Key 的对称加密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

type Cipher struct {
	aead cipher.AEAD
}

func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AES-256 需要 32 字节密钥，当前 %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt 输出 nonce || ciphertext，直接存 bytea。
func (c *Cipher) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (c *Cipher) Decrypt(data []byte) (string, error) {
	ns := c.aead.NonceSize()
	if len(data) <= ns {
		return "", errors.New("密文长度非法")
	}
	out, err := c.aead.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", errors.New("解密失败：密钥可能已更换")
	}
	return string(out), nil
}

// Mask 只保留尾部 4 位，用于接口回显。
func Mask(secret string) string {
	if len(secret) <= 4 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}
