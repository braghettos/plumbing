package request

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/krateo-platformops/plumbing/endpoints"
)

// cloneRequest creates a shallow copy of the request along with a deep copy of the Headers.
func cloneRequest(req *http.Request) *http.Request {
	r := new(http.Request)

	// shallow clone
	*r = *req

	// deep copy headers
	r.Header = cloneHeader(req.Header)

	return r
}

// cloneHeader creates a deep copy of an http.Header.
func cloneHeader(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for key, values := range in {
		newValues := make([]string, len(values))
		copy(newValues, values)
		out[key] = newValues
	}
	return out
}

// isTextResponse returns true if the response appears to be a textual media type.
func isTextResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	if len(contentType) == 0 {
		return true
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(media, "text/")
}

func ComputeAwsHeaders(ep *endpoints.Endpoint, ex *RequestInfo) ([]string, string, string, string, string, string) {
	var dateStamp, amzDate string
	if len(ep.AwsTime) != 0 {
		dateStamp = ep.AwsTime
		// If AwsTime is just YYYYMMDD, construct full timestamp
		if len(ep.AwsTime) == 8 {
			amzDate = ep.AwsTime + "T000000Z"
		} else {
			amzDate = ep.AwsTime
		}
	} else {
		t := time.Now().UTC()
		dateStamp = t.Format("20060102")
		amzDate = t.Format("20060102T150405Z")
	}
	url, _ := url.Parse(ep.ServerURL)
	host := url.Host
	if host == "" {
		host = "localhost"
	}

	canonicalURI := ex.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Step 2: Build headers
	// Empty payload hash (SHA256 of empty string): "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	payloadHash := ""
	if ex.Payload != nil {
		payloadHash = fmt.Sprintf("%x", sha256.Sum256([]byte(*ex.Payload)))
	} else {
		payloadHash = fmt.Sprintf("%x", sha256.Sum256([]byte("")))
	}

	return []string{
		"host:" + host,
		"x-amz-content-sha256:" + payloadHash,
		"x-amz-date:" + amzDate,
	}, dateStamp, amzDate, host, canonicalURI, payloadHash
}

func ComputeAwsSignature(ep *endpoints.Endpoint, ex *RequestInfo) string {
	// Docs:
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-authenticating-requests.html
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-auth-using-authorization-header.html
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html

	_, dateStamp, amzDate, _, canonicalURI, payloadHash := ComputeAwsHeaders(ep, ex)

	headers := map[string]string{}

	for _, v := range ex.Headers {
		split := strings.Split(v, ":")
		headers[strings.Trim(split[0], " ")] = strings.Trim(split[1], " ")
	}

	// Build canonical headers and signed headers list
	var headerKeys []string
	for k := range headers {
		headerKeys = append(headerKeys, k)
	}

	sort.Strings(headerKeys)

	// fmt.Printf("headerKeys: \n%s\n\n", headerKeys)
	// fmt.Printf("Header: \n%s\n\n", ex.Headers)

	var canonicalHeaders strings.Builder
	for _, k := range headerKeys {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(headers[k])
		canonicalHeaders.WriteString("\n")
	}

	signedHeaders := strings.Join(headerKeys, ";")

	// Step 3: Build canonical request
	method := *ex.Verb
	canonicalQueryString := ""

	canonicalRequest := method + "\n" +
		canonicalURI + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders.String() + "\n" +
		signedHeaders + "\n" +
		payloadHash

	// Step 4: Create string to sign
	algorithm := "AWS4-HMAC-SHA256"
	credentialScope := dateStamp + "/" + ep.AwsRegion + "/" + ep.AwsService + "/aws4_request"

	hashedCanonicalRequest := fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalRequest)))

	stringToSign := algorithm + "\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		hashedCanonicalRequest

	// Step 5: Calculate signature
	signingKey := getHMAC([]byte("AWS4"+ep.AwsSecretKey), []byte(dateStamp))
	signingKey = getHMAC(signingKey, []byte(ep.AwsRegion))
	signingKey = getHMAC(signingKey, []byte(ep.AwsService))
	signingKey = getHMAC(signingKey, []byte("aws4_request"))

	signature := getHMAC(signingKey, []byte(stringToSign))
	signatureHex := hex.EncodeToString(signature)

	// Step 6: Build authorization header
	authorization := algorithm + " " +
		"Credential=" + ep.AwsAccessKey + "/" + credentialScope + "," +
		"SignedHeaders=" + signedHeaders + "," +
		"Signature=" + signatureHex

	// fmt.Printf("canonicalRequest \n%s\n\n", canonicalRequest)
	// fmt.Printf("stringToSign \n%s\n\n", stringToSign)
	// fmt.Printf("signatureHex \n%s\n\n", signatureHex)

	return authorization
}

func getHMAC(key []byte, data []byte) []byte {
	hash := hmac.New(sha256.New, key)
	hash.Write(data)
	return hash.Sum(nil)
}
