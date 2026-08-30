package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	iso8601BasicFormat = "20060102T150405Z"
	dateStampFormat    = "20060102"
	authHeaderPrefix   = "AWS4-HMAC-SHA256"
	awsServiceS3       = "s3"
)

// SignRequest applies AWS Signature Version 4 (SigV4) headers to an HTTP request.
func SignRequest(req *http.Request, bodyHash string, accessKey, secretKey, region string, now time.Time) {
	if region == "" {
		region = "us-east-1"
	}
	if bodyHash == "" {
		emptyHash := sha256.Sum256([]byte{})
		bodyHash = hex.EncodeToString(emptyHash[:])
	}

	dateStamp := now.UTC().Format(dateStampFormat)
	amzDate := now.UTC().Format(iso8601BasicFormat)

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", bodyHash)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}

	// 1. Canonical Headers & Signed Headers
	headersToSign := make(map[string]string)
	for k, vals := range req.Header {
		lowerK := strings.ToLower(k)
		if lowerK == "host" || lowerK == "content-type" || strings.HasPrefix(lowerK, "x-amz-") {
			trimmedVals := make([]string, len(vals))
			for i, v := range vals {
				trimmedVals[i] = strings.TrimSpace(v)
			}
			headersToSign[lowerK] = strings.Join(trimmedVals, ",")
		}
	}
	if _, ok := headersToSign["host"]; !ok {
		headersToSign["host"] = req.URL.Host
	}

	var signedHeaderKeys []string
	for k := range headersToSign {
		signedHeaderKeys = append(signedHeaderKeys, k)
	}
	sort.Strings(signedHeaderKeys)

	var canonicalHeaders strings.Builder
	for _, k := range signedHeaderKeys {
		canonicalHeaders.WriteString(fmt.Sprintf("%s:%s\n", k, headersToSign[k]))
	}
	signedHeadersStr := strings.Join(signedHeaderKeys, ";")

	// 2. Canonical URI
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	} else if !strings.HasPrefix(canonicalURI, "/") {
		canonicalURI = "/" + canonicalURI
	}
	canonicalURI = uriEncodePath(canonicalURI)

	// 3. Canonical Query String
	canonicalQuery := buildCanonicalQuery(req.URL.Query())

	// 4. Canonical Request
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeadersStr,
		bodyHash,
	)
	canonicalRequestHash := sha256Hex([]byte(canonicalRequest))

	// 5. String to Sign
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, awsServiceS3)
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		authHeaderPrefix,
		amzDate,
		credentialScope,
		canonicalRequestHash,
	)

	// 6. Signing Key Derivation
	signingKey := deriveSigningKey(secretKey, dateStamp, region, awsServiceS3)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 7. Authorization Header
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		authHeaderPrefix,
		accessKey,
		credentialScope,
		signedHeadersStr,
		signature,
	)

	req.Header.Set("Authorization", authHeader)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func buildCanonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var pairs []string
	for _, k := range keys {
		vals := values[k]
		sort.Strings(vals)
		if len(vals) == 0 {
			pairs = append(pairs, url.QueryEscape(k)+"=")
		} else {
			for _, v := range vals {
				pairs = append(pairs, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
			}
		}
	}
	return strings.Join(pairs, "&")
}

func uriEncodePath(path string) string {
	var segments []string
	for _, seg := range strings.Split(path, "/") {
		segments = append(segments, url.PathEscape(seg))
	}
	return strings.Join(segments, "/")
}
