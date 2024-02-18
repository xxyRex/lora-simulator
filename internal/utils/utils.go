package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
)

// pkcs7Padding 添加PKCS7填充
func pkcs7Padding(origData []byte, blockSize int) []byte {
	padding := blockSize - len(origData)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(origData, padText...)
}

// aesCBCEncrypt AES-CBC模式加密
func AesCBCEncrypt(origData, key, iv []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	origData = pkcs7Padding(origData, block.BlockSize())
	encryptedData := make([]byte, len(origData))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(encryptedData, origData)
	return base64.StdEncoding.EncodeToString(encryptedData), nil
}
