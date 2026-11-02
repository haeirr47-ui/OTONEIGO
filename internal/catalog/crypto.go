package catalog

// WEAPI protocol constants adapted from go-musicfox/netease-music util/cryto.go
// (MIT). See docs/netease-adapter.md for the full upstream notice.
// Implementation uses only Go's standard cryptographic primitives.
import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/url"
)

func encryptCBC(data, key []byte) string {
	n := aes.BlockSize - len(data)%aes.BlockSize
	for i := 0; i < n; i++ {
		data = append(data, byte(n))
	}
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, []byte("0102030405060708")).CryptBlocks(out, data)
	return base64.StdEncoding.EncodeToString(out)
}
func weapi(data any) (url.Values, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	secret := make([]byte, 16)
	reverse := make([]byte, 16)
	for i := range secret {
		n, e := rand.Int(rand.Reader, big.NewInt(62))
		if e != nil {
			return nil, e
		}
		secret[i] = alphabet[n.Int64()]
		reverse[15-i] = secret[i]
	}
	modulus, _ := new(big.Int).SetString("00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7", 16)
	encrypted := new(big.Int).Exp(new(big.Int).SetBytes(reverse), big.NewInt(65537), modulus).FillBytes(make([]byte, 128))
	return url.Values{"params": {encryptCBC([]byte(encryptCBC(raw, []byte{48, 67, 111, 74, 85, 109, 54, 81, 121, 119, 56, 87, 56, 106, 117, 100})), secret)}, "encSecKey": {hex.EncodeToString(encrypted)}}, nil
}
