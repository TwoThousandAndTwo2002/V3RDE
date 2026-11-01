package post

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The example of the AWS guide "Create a signed AWS API request" (IAM
// ListUsers, 2015-08-30T12:36:00Z, the example secret key).
func TestSigV4AWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	c := AWSCreds{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	SignV4(req, nil, c, "us-east-1", "iam", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, " +
		"SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization\n got %s\nwant %s", got, want)
	}
}

func TestSigV4TokenAndPath(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://abc.execute-api.us-east-1.amazonaws.com/prod/%40connections/L0SM9cOFvHcCIhw%3D", nil)
	c := AWSCreds{AccessKeyID: "AKID", SecretAccessKey: "s", SessionToken: "tok"}
	SignV4(req, []byte(`{"k":1}`), c, "us-east-1", "execute-api", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if req.Header.Get("X-Amz-Security-Token") != "tok" || !strings.Contains(req.Header.Get("Authorization"), "SignedHeaders=host;x-amz-date;x-amz-security-token,") {
		t.Fatalf("%v", req.Header)
	}
	if req.URL.EscapedPath() != "/prod/%40connections/L0SM9cOFvHcCIhw%3D" { // the form manage sends (the AWS guide)
		t.Fatalf("the path sent: %s", req.URL.EscapedPath())
	}
	if awsEscape(req.URL.EscapedPath(), false) != "/prod/%2540connections/L0SM9cOFvHcCIhw%253D" {
		t.Fatalf("canonical path %s", awsEscape(req.URL.EscapedPath(), false))
	}
}

func TestIMDSCreds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/latest/api/token" && r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds") != "":
			_, _ = w.Write([]byte("T"))
		case r.Header.Get("X-aws-ec2-metadata-token") != "T":
			w.WriteHeader(401)
		case r.URL.Path == "/latest/meta-data/iam/security-credentials/":
			_, _ = w.Write([]byte("box-role\n"))
		case r.URL.Path == "/latest/meta-data/iam/security-credentials/box-role":
			_, _ = w.Write([]byte(`{"Code":"Success","AccessKeyId":"AK","SecretAccessKey":"SK","Token":"TK","Expiration":"` +
				time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	m := &IMDSCreds{Base: srv.URL}
	c, err := m.Creds(context.Background())
	if err != nil || c.AccessKeyID != "AK" || c.SecretAccessKey != "SK" || c.SessionToken != "TK" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := m.Creds(context.Background()); err != nil || calls != 3 {
		t.Fatalf("the cache: %d calls %v", calls, err)
	}
	bad := &IMDSCreds{Base: srv.URL + "/none"}
	if _, err := bad.Creds(context.Background()); err == nil {
		t.Fatal("no error from a bad IMDS")
	}
}
