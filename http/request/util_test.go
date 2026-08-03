package request

import (
	"net/http"
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
)

func TestCloneRequest(t *testing.T) {
	orig := &http.Request{
		Header: http.Header{
			"Test-Header": []string{"value1", "value2"},
		},
	}
	clone := cloneRequest(orig)

	if &orig == &clone {
		t.Errorf("CloneRequest did not create a new request instance")
	}
	if &orig.Header == &clone.Header {
		t.Errorf("CloneRequest did not create a deep copy of headers")
	}
	if len(clone.Header.Get("Test-Header")) == 0 {
		t.Errorf("CloneRequest did not copy headers correctly")
	}
}

func TestCloneHeader(t *testing.T) {
	orig := http.Header{
		"Test-Header": []string{"value1", "value2"},
	}
	clone := cloneHeader(orig)

	if orig.Get("Test-Header") != clone.Get("Test-Header") {
		t.Errorf("cloneHeader did not create a deep copy of header values")
	}
}

func TestIsTextResponse(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expectText  bool
	}{
		{"empty content type", "", true},
		{"text/plain", "text/plain", true},
		{"text/html", "text/html", true},
		{"application/json", "application/json", false},
		{"invalid content type", "invalid/type", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{"Content-Type": []string{tc.contentType}}}
			if isTextResponse(resp) != tc.expectText {
				t.Errorf("unexpected result for %s: got %v, want %v", tc.contentType, !tc.expectText, tc.expectText)
			}
		})
	}
}

func TestComputeAwsHeader(t *testing.T) {
	// Data from documentation example
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html
	payload := ""
	verb := "GET"

	opts := RequestOptions{
		Endpoint: &endpoints.Endpoint{
			ServerURL:    "https://examplebucket.s3.amazonaws.com",
			AwsAccessKey: "AKIAIOSFODNN7EXAMPLE",
			AwsSecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			AwsRegion:    "us-east-1",
			AwsService:   "s3",
			AwsTime:      "20130524",
		},
		RequestInfo: RequestInfo{
			Headers: []string{
				"range: bytes=0-9",
			},
			Path:    "/test.txt",
			Payload: &payload,
			Verb:    &verb,
		},
	}
	headers, _, _, _, _, _ := ComputeAwsHeaders(opts.Endpoint, &opts.RequestInfo)

	opts.Headers = append(opts.Headers, headers...)

	expected := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"

	actual := ComputeAwsSignature(opts.Endpoint, &opts.RequestInfo)
	if actual != expected {
		t.Errorf("unexpected result in AWS signature computation: expected %s\ngot %s", expected, actual)
	}

}
