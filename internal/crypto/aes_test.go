package crypto

import (
	"strings"
	"testing"
)

func testKey(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestEncryptDecrypt(t *testing.T) {
	c, err := New(testKey(7))
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-1234567890abcdef"
	cipherText, err := c.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cipherText), secret) {
		t.Fatal("密文中不应出现明文")
	}
	plain, err := c.Decrypt(cipherText)
	if err != nil {
		t.Fatal(err)
	}
	if plain != secret {
		t.Fatalf("解密结果不匹配: %q", plain)
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	c1, _ := New(testKey(1))
	c2, _ := New(testKey(2))
	cipherText, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(cipherText); err == nil {
		t.Fatal("不同密钥必须解密失败")
	}
}

func TestMask(t *testing.T) {
	if Mask("sk-abcdef123456") != "****3456" {
		t.Fatalf("掩码结果异常: %s", Mask("sk-abcdef123456"))
	}
	if Mask("ab") != "****" {
		t.Fatal("过短密钥应整体掩码")
	}
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("密钥长度错误应报错")
	}
}
