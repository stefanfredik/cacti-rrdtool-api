package querysign

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// SignQuery signs the query parameters with path and secret key.
func SignQuery(secret, path, query []byte) []byte {
	var buf bytes.Buffer
	buf.Write(query)
	if len(query) != 0 && query[len(query)-1] != '&' {
		buf.WriteByte('&')
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(path)
	_, _ = mac.Write([]byte{'?'})
	_, _ = mac.Write(buf.Bytes())
	sig := mac.Sum(nil)
	hexSig := [64]byte{}
	hex.Encode(hexSig[:], sig)
	buf.WriteString("s=")
	buf.Write(hexSig[:])
	return buf.Bytes()
}

// ValidateSignedQuery validates the signature in query string.
func ValidateSignedQuery(secret, path, query []byte) bool {
	sigStart := len(query) - 66 // ...s=$hmac
	if sigStart < 0 {
		return false // not long enough.
	}
	if query[sigStart] != 's' || query[sigStart+1] != '=' {
		return false // 's=' must come before signature.
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(path)
	_, _ = mac.Write([]byte{'?'})
	_, _ = mac.Write(query[:sigStart])
	expected := mac.Sum(nil)
	actual := [32]byte{}
	if _, err := hex.Decode(actual[:], query[sigStart+2:]); err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(expected, actual[:]) == 1
}
