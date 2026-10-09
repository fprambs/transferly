// Package azure is the small client of the Azure Storage REST APIs the Blob and Files adapters share (ADR-0025): the URL of
// an endpoint, Shared Key signing following the documented string to sign, SAS tokens, and the errors of the service read as
// the errors of the port.
package azure

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fprambs/transferly/internal/core/netguard"
	"github.com/fprambs/transferly/internal/core/remotefs"
)

// APIVersion is the version of the REST APIs every request asks for.
const APIVersion = "2023-11-03"

// errorReadTimeout bounds a remote error body even when it stalls; tests use a shorter budget.
var errorReadTimeout = 10 * time.Second

// Config tells how to reach a service of a storage account.
type Config struct {
	// Endpoint is the base URL of the service: https://{account}.blob.core.windows.net, or an emulator with the account in its
	// path, such as http://azurite:10000/{account}.
	Endpoint string
	// Account is the name of the storage account.
	Account string
	// Key is the account key, in base64, which signs the requests with Shared Key; empty when another credential is used.
	Key string
	// SAS is a shared access signature, the query string of a SAS token, used when there is no key; both empty means anonymous.
	SAS string
	// Client sends the requests.
	Client *http.Client
}

// Endpoint returns the base URL of a service (blob or file) of an account: the default endpoint of Azure when host is empty,
// host as an account host when it starts with the name of the account, or host as an emulator that takes the account in the path.
func Endpoint(service, account, host string, port int, secure bool) string {
	scheme := "http"
	if secure {
		scheme = "https"
	}
	if host == "" {
		return fmt.Sprintf("https://%s.%s.core.windows.net", account, service)
	}
	address := host
	if (secure && port != 443) || (!secure && port != 80) {
		address = fmt.Sprintf("%s:%d", host, port)
	}
	if strings.HasPrefix(host, account+".") {
		return scheme + "://" + address
	}
	return scheme + "://" + address + "/" + account
}

// Client sends signed requests to a service. It is safe for concurrent use.
type Client struct {
	base    *url.URL
	account string
	key     []byte
	sas     url.Values
	http    *http.Client
}

// New returns the client of cfg; a key that is not base64 or a SAS token that is not a query string is invalid.
func New(cfg Config) (*Client, error) {
	base, err := url.Parse(strings.TrimSuffix(cfg.Endpoint, "/"))
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("azure endpoint %q: %w", cfg.Endpoint, remotefs.ErrInvalid)
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("azure account key: %w: it is not base64", remotefs.ErrInvalid)
	}
	sas, err := url.ParseQuery(strings.TrimPrefix(cfg.SAS, "?"))
	if err != nil {
		return nil, fmt.Errorf("azure SAS token: %w: %w", remotefs.ErrInvalid, err)
	}
	return &Client{base: base, account: cfg.Account, key: key, sas: sas, http: cfg.Client}, nil
}

// URL returns the URL of a resource, the escaped path of its share or container and name, with the SAS token when there is one.
func (c *Client) URL(resource string, query url.Values) string {
	u := *c.base
	u.Path = c.base.Path + "/" + strings.TrimPrefix(resource, "/")
	values := url.Values{}
	for k, v := range query {
		values[k] = v
	}
	if len(c.key) == 0 {
		for k, v := range c.sas {
			values[k] = v
		}
	}
	u.RawQuery = values.Encode()
	return u.String()
}

// Do sends a request for a resource, signed with Shared Key when the client has a key, and returns its answer when it
// succeeded; a failure the service answers is an [*Error].
func (c *Client) Do(ctx context.Context, method, resource string, query url.Values, header http.Header, body []byte) (*http.Response, error) {
	var reader io.Reader = http.NoBody
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL(resource, query), reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", remotefs.ErrInvalid, err)
	}
	for name, values := range header {
		req.Header[http.CanonicalHeaderKey(name)] = values
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("x-ms-version", APIVersion)
	if len(c.key) > 0 {
		req.Header.Set("Authorization", "SharedKey "+c.account+":"+Sign(req, c.account, c.key))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer func() { _ = resp.Body.Close() }()
		return nil, failure(resp)
	}
	return resp, nil
}

// Sign returns the Shared Key signature of a request: the HMAC-SHA256, with the account key, of its string to sign.
func Sign(req *http.Request, account string, key []byte) string {
	length := ""
	if req.ContentLength > 0 {
		length = strconv.FormatInt(req.ContentLength, 10)
	}
	h := req.Header
	lines := []string{
		req.Method, h.Get("Content-Encoding"), h.Get("Content-Language"), length, h.Get("Content-MD5"), h.Get("Content-Type"), "",
		h.Get("If-Modified-Since"), h.Get("If-Match"), h.Get("If-None-Match"), h.Get("If-Unmodified-Since"), h.Get("Range"),
	}
	var names []string
	for name := range h {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, "x-ms-") {
			names = append(names, lower)
		}
	}
	slices.Sort(names)
	var canonical strings.Builder
	for _, name := range names {
		canonical.WriteString(name + ":" + strings.TrimSpace(h.Get(name)) + "\n")
	}
	resource := "/" + account + req.URL.EscapedPath()
	query := req.URL.Query()
	params := make([]string, 0, len(query))
	for name := range query {
		params = append(params, name)
	}
	slices.Sort(params)
	for _, name := range params {
		values := slices.Clone(query[name])
		slices.Sort(values)
		resource += "\n" + strings.ToLower(name) + ":" + strings.Join(values, ",")
	}
	toSign := strings.Join(lines, "\n") + "\n" + canonical.String() + resource
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Error is a failure the service answered, with its HTTP status and the code of Azure Storage.
type Error struct {
	Status int
	Code   string
}

// Error implements error.
func (e *Error) Error() string { return fmt.Sprintf("azure answered %d %s", e.Status, e.Code) }

// failure reads the error of an answer: the code of its x-ms-error-code header, or of its XML body when the header is missing.
func failure(resp *http.Response) error {
	code := resp.Header.Get("x-ms-error-code")
	if code == "" {
		var body struct {
			Code string `xml:"Code"`
		}
		deadline := time.AfterFunc(errorReadTimeout, func() { _ = resp.Body.Close() })
		defer deadline.Stop()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = xml.Unmarshal(raw, &body)
		code = body.Code
	}
	return &Error{Status: resp.StatusCode, Code: code}
}

// Code returns the code of Azure Storage a failure carries, or empty when it is not a failure the service answered.
func Code(err error) string {
	var answer *Error
	if errors.As(err, &answer) {
		return answer.Code
	}
	return ""
}

// The codes of Azure Storage the adapters tell apart.
var (
	missing   = []string{"BlobNotFound", "ContainerNotFound", "ResourceNotFound", "ShareNotFound", "ParentNotFound"}
	refused   = []string{"AuthenticationFailed", "InvalidAuthenticationInfo", "NoAuthenticationInformation"}
	forbidden = []string{"AuthorizationFailure", "AuthorizationPermissionMismatch", "AuthorizationResourceTypeMismatch", "AuthorizationServiceMismatch", "InsufficientAccountPermissions", "PublicAccessNotPermitted"}
	existing  = []string{"BlobAlreadyExists", "ResourceAlreadyExists", "ConditionNotMet"}
	full      = []string{"ShareSizeLimitReached", "AccountSizeLimitReached"}
)

// Wrap turns a failure into the errors of the port, keeping cancellations as they are and destinations the network guard
// refused as [netguard.ErrBlocked], which trying again cannot fix.
func Wrap(op, p string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, remotefs.ErrInvalid) {
		return err
	}
	if errors.Is(err, netguard.ErrBlocked) {
		return fmt.Errorf("%s %q: %w", op, p, err)
	}
	kind := remotefs.ErrConnectionLost
	var answer *Error
	if errors.As(err, &answer) {
		switch {
		case slices.Contains(missing, answer.Code) || (answer.Code == "" && answer.Status == http.StatusNotFound):
			kind = remotefs.ErrNotFound
		case slices.Contains(refused, answer.Code):
			kind = remotefs.ErrAuth
		case slices.Contains(forbidden, answer.Code) || (answer.Code == "" && answer.Status == http.StatusForbidden):
			kind = remotefs.ErrPermission
		case slices.Contains(existing, answer.Code):
			kind = remotefs.ErrExists
		case answer.Code == "DirectoryNotEmpty":
			kind = remotefs.ErrNotEmpty
		case slices.Contains(full, answer.Code):
			kind = remotefs.ErrQuota
		case answer.Status < http.StatusInternalServerError && answer.Status != http.StatusTooManyRequests:
			kind = remotefs.ErrInvalid
		}
	}
	return fmt.Errorf("%s %q: %w: %w", op, p, kind, err)
}
