package post

// WS1: AWS Signature Version 4 for the API Gateway management API (POST and
// DELETE @connections/{id}), and the box role's credentials from the
// instance metadata (IMDSv2). Written here with the standard library only
// (the module has no AWS SDK); the signing steps are those of
// https://docs.aws.amazon.com/IAM/latest/UserGuide/create-signed-request.html
// and TestSigV4AWSExample checks them against the example of that guide.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// AWSCreds are temporary (or long-lived) AWS credentials.
type AWSCreds struct {
	AccessKeyID, SecretAccessKey, SessionToken string
	Expires                                    time.Time // zero = does not expire
}

// CredSource gives the current credentials.
type CredSource interface {
	Creds(ctx context.Context) (AWSCreds, error)
}

func sha256hexb(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func hmacSHA(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

// awsEscape is the URI encoding of SigV4: every byte except the unreserved
// characters A-Z a-z 0-9 - _ . ~ is %XX (upper case).
func awsEscape(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !slash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// SignV4 signs req (body is its payload) for service and region at t. It
// sets X-Amz-Date, X-Amz-Security-Token (temporary credentials) and
// Authorization. The signed headers are host, x-amz-date, the security token
// and content-type when set.
func SignV4(req *http.Request, body []byte, c AWSCreds, region, service string, t time.Time) {
	amzDate := t.UTC().Format("20060102T150405Z")
	day := amzDate[:8]
	req.Header.Set("X-Amz-Date", amzDate)
	if c.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.SessionToken)
	}
	host := req.URL.Host
	if req.Host != "" {
		host = req.Host
	}
	heads := map[string]string{"host": host, "x-amz-date": amzDate}
	if c.SessionToken != "" {
		heads["x-amz-security-token"] = c.SessionToken
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		heads["content-type"] = strings.TrimSpace(ct)
	}
	names := make([]string, 0, len(heads))
	for k := range heads {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k + ":" + heads[k] + "\n")
	}
	signed := strings.Join(names, ";")
	// the query, sorted by name then value, each part encoded
	q := req.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var qs []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			qs = append(qs, awsEscape(k, true)+"="+awsEscape(v, true))
		}
	}
	// the canonical path is the path as sent, encoded once more (every
	// service but S3; botocore does the same: quote(path, safe="/~"))
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if service != "s3" {
		path = awsEscape(path, false)
	}
	canon := strings.Join([]string{req.Method, path, strings.Join(qs, "&"), ch.String(), signed, sha256hexb(body)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256hexb([]byte(canon))
	k := hmacSHA([]byte("AWS4"+c.SecretAccessKey), day)
	k = hmacSHA(k, region)
	k = hmacSHA(k, service)
	k = hmacSHA(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.AccessKeyID+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
}

// IMDSCreds reads the instance role's credentials from IMDSv2 and keeps
// them until 5 minutes before they expire. A container reaches IMDS only
// when the instance's metadata hop limit is 2 or more.
type IMDSCreds struct {
	Base string // http://169.254.169.254 (tests: a fake)
	HTTP *http.Client
	mu   sync.Mutex
	c    AWSCreds
}

func (m *IMDSCreds) get(ctx context.Context, method, path, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.Base, "/")+path, nil)
	if err != nil {
		return "", err
	}
	if token == "" {
		req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	} else {
		req.Header.Set("X-aws-ec2-metadata-token", token)
	}
	hc := m.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("imds %s: HTTP %d", path, resp.StatusCode)
	}
	return strings.TrimSpace(string(b)), nil
}

// Creds gives the role's credentials (from the cache while they are good
// for 5 more minutes).
func (m *IMDSCreds) Creds(ctx context.Context) (AWSCreds, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.c.AccessKeyID != "" && time.Until(m.c.Expires) > 5*time.Minute {
		return m.c, nil
	}
	tok, err := m.get(ctx, http.MethodPut, "/latest/api/token", "")
	if err != nil {
		return AWSCreds{}, err
	}
	roles, err := m.get(ctx, http.MethodGet, "/latest/meta-data/iam/security-credentials/", tok)
	if err != nil {
		return AWSCreds{}, err
	}
	role := strings.SplitN(roles, "\n", 2)[0]
	if role == "" {
		return AWSCreds{}, errors.New("imds: the instance has no role")
	}
	raw, err := m.get(ctx, http.MethodGet, "/latest/meta-data/iam/security-credentials/"+role, tok)
	if err != nil {
		return AWSCreds{}, err
	}
	var j struct {
		Code, AccessKeyId, SecretAccessKey, Token, Expiration string
	}
	if err := json.Unmarshal([]byte(raw), &j); err != nil || j.AccessKeyId == "" {
		return AWSCreds{}, fmt.Errorf("imds: bad credentials answer (code %q)", j.Code)
	}
	exp, err := time.Parse(time.RFC3339, j.Expiration)
	if err != nil { // keep them a short time, so the cache still works
		exp = time.Now().Add(10 * time.Minute)
	}
	m.c = AWSCreds{AccessKeyID: j.AccessKeyId, SecretAccessKey: j.SecretAccessKey, SessionToken: j.Token, Expires: exp}
	return m.c, nil
}
