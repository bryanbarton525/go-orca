// streaming-e2e validates JWT ingest → Redpanda for homelab deployments.
//
// Usage:
//
//	kubectl -n zitadel get secret iam-admin -o jsonpath='{.data.iam-admin\.json}' | base64 -d > /tmp/iam-admin.json
//	go run ./hack/streaming-e2e -sa /tmp/iam-admin.json -events https://events.iambarton.com
package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type serviceAccount struct {
	Type      string `json:"type"`
	KeyID     string `json:"keyId"`
	Key       string `json:"key"`
	UserID    string `json:"userId"`
	ExpiresAt string `json:"expirationDate"`
}

func main() {
	saPath := flag.String("sa", "", "path to ZITADEL service account JSON (iam-admin.json)")
	pat := flag.String("pat", "", "ZITADEL personal access token (skips JWT bearer exchange)")
	issuer := flag.String("issuer", "https://id.iambarton.com", "ZITADEL issuer / audience")
	eventsURL := flag.String("events", "https://events.iambarton.com/api/v1/events", "event ingest URL")
	flag.Parse()

	var accessToken string
	switch {
	case strings.TrimSpace(*pat) != "":
		accessToken = strings.TrimSpace(*pat)
	case *saPath != "":
		sa, err := loadServiceAccount(*saPath)
		if err != nil {
			fatal(err)
		}
		token, err := fetchAccessToken(sa, strings.TrimRight(*issuer, "/"))
		if err != nil {
			fatal(err)
		}
		accessToken = token
	default:
		fmt.Fprintln(os.Stderr, "one of -sa or -pat is required")
		os.Exit(2)
	}

	marker := fmt.Sprintf("streaming-e2e-%d", time.Now().Unix())
	body := []byte(`{"source":"streaming-e2e","marker":"` + marker + `"}`)
	req, err := http.NewRequest(http.MethodPost, *eventsURL, bytes.NewReader(body))
	if err != nil {
		fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		fatal(fmt.Errorf("ingest returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))))
	}

	fmt.Printf("ingest ok marker=%s\n", marker)
}

func loadServiceAccount(path string) (*serviceAccount, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sa serviceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, err
	}
	if sa.UserID == "" || sa.KeyID == "" || sa.Key == "" {
		return nil, fmt.Errorf("service account JSON missing userId, keyId, or key")
	}
	return &sa, nil
}

func fetchAccessToken(sa *serviceAccount, issuer string) (string, error) {
	assertion, err := signAssertion(sa, issuer)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("scope", "openid")
	form.Set("assertion", assertion)

	resp, err := http.PostForm(issuer+"/oauth/v2/token", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}
	return payload.AccessToken, nil
}

func signAssertion(sa *serviceAccount, issuer string) (string, error) {
	key, err := parseRSAPrivateKey(sa.Key)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	header, err := json.Marshal(map[string]string{
		"alg": "RS256",
		"kid": sa.KeyID,
	})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss": sa.UserID,
		"sub": sa.UserID,
		"aud": issuer,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}

	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parseRSAPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(pemText, `\n`, "\n")))
	if block == nil {
		return nil, fmt.Errorf("decode PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("expected RSA private key")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "streaming-e2e:", err)
	os.Exit(1)
}
