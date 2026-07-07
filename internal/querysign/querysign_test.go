package querysign

import (
	"bytes"
	"net/url"
	"testing"
)

func TestQuerySign(t *testing.T) {
	secret := []byte("my_super_secret_key_12345")
	path := []byte("/api/v1/graph")
	queryParams := "graph=localhost_mem_buffers_3.rrd&x=1751912400"
	query := []byte(queryParams)

	// 1. Sign query
	signed := SignQuery(secret, path, query)

	// Verify that the signed query ends with s=signature
	if !bytes.Contains(signed, []byte("s=")) {
		t.Fatalf("Signed query does not contain signature parameter: %s", signed)
	}

	// 2. Validate valid signed query
	if !ValidateSignedQuery(secret, path, signed) {
		t.Errorf("Validation failed for a valid signed query: %s", signed)
	}

	// 3. Validation should fail with wrong secret
	wrongSecret := []byte("wrong_secret_key_9999")
	if ValidateSignedQuery(wrongSecret, path, signed) {
		t.Errorf("Validation should have failed for a wrong secret key")
	}

	// 4. Validation should fail with modified path
	wrongPath := []byte("/api/v1/xport")
	if ValidateSignedQuery(secret, wrongPath, signed) {
		t.Errorf("Validation should have failed for a modified path")
	}

	// 5. Validation should fail if query parameters are modified
	parsed, err := url.ParseQuery(string(signed))
	if err != nil {
		t.Fatalf("Failed to parse signed query: %v", err)
	}
	// Change parameter
	parsed.Set("x", "1751912401")
	modifiedQuery := []byte(parsed.Encode())
	if ValidateSignedQuery(secret, path, modifiedQuery) {
		t.Errorf("Validation should have failed for modified query parameters")
	}

	// 6. Validation should fail if signature is invalid hex
	invalidHexSig := signed[:len(signed)-64]
	invalidHexSig = append(invalidHexSig, bytes.Repeat([]byte("g"), 64)...) // 'g' is not valid hex
	if ValidateSignedQuery(secret, path, invalidHexSig) {
		t.Errorf("Validation should have failed for invalid hex signature")
	}

	// 7. Validation should fail for too short query
	if ValidateSignedQuery(secret, path, []byte("s=123")) {
		t.Errorf("Validation should have failed for query that is too short")
	}
}
