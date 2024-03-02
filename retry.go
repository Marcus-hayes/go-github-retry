package retry

import (
	"context"
	"crypto/x509"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

var (
	// A regular expression to match the error returned by net/http when the
	// configured number of redirects is exhausted. This error isn't typed
	// specifically so we resort to matching on the error string.
	redirectsErrorRe = regexp.MustCompile(`stopped after \d+ redirects\z`)

	// A regular expression to match the error returned by net/http when the
	// scheme specified in the URL is invalid. This error isn't typed
	// specifically so we resort to matching on the error string.
	schemeErrorRe = regexp.MustCompile(`unsupported protocol scheme`)

	// A regular expression to match the error returned by net/http when the
	// TLS certificate is not trusted. This error isn't typed
	// specifically so we resort to matching on the error string.
	notTrustedErrorRe    = regexp.MustCompile(`certificate is not trusted`)
	DefaultMinBackoff    = 1 * time.Minute
	DefaultMaxBackoff    = 8 * time.Minute
	DefaultRetryAttempts = 3
)

func NewClient() *http.Client {
	rClient := retryablehttp.NewClient()
	rClient.RetryMax = DefaultRetryAttempts
	rClient.RetryWaitMin = DefaultMinBackoff
	rClient.RetryWaitMax = DefaultMaxBackoff
	rClient.Backoff = func(min, max time.Duration, attemptNum int, resp *http.Response) time.Duration {
		if resp != nil {
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode >= 500 {
				if s, ok := resp.Header["Retry-After"]; ok {
					if sleep, err := strconv.ParseInt(s[0], 10, 64); err == nil {
						return time.Second * time.Duration(sleep)
					}
				}
				rSlc := resp.Header["X-Ratelimit-Remaining"]
				remainingRateQuota, _ := strconv.ParseInt(rSlc[0], 10, 64)
				if remainingRateQuota == 0 {
					quotaResetTimeSlc := resp.Header["X-Ratelimit-Reset"]
					quotaResetTimeInt, _ := strconv.ParseInt(quotaResetTimeSlc[0], 10, 64)
					return time.Second * (time.Until(time.Unix(quotaResetTimeInt, 0)))
				}
			}
		}
		// Default Exponential Backoff, starting with 1m sleep duration
		mult := math.Pow(2, float64(attemptNum)) * float64(min)
		sleep := time.Duration(mult)
		if float64(sleep) != mult || sleep > max {
			sleep = max
		}
		return sleep
	}
	rClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		// do not retry on context.Canceled or context.DeadlineExceeded
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil {
			if v, ok := err.(*url.Error); ok {
				// Don't retry if the error was due to too many redirects.
				if redirectsErrorRe.MatchString(v.Error()) {
					return false, v
				}

				// Don't retry if the error was due to an invalid protocol scheme.
				if schemeErrorRe.MatchString(v.Error()) {
					return false, v
				}

				// Don't retry if the error was due to TLS cert verification failure.
				if notTrustedErrorRe.MatchString(v.Error()) {
					return false, v
				}
				if _, ok := v.Err.(x509.UnknownAuthorityError); ok {
					return false, v
				}
			}

			// The error is likely recoverable so retry.
			return true, nil
		}

		// 429 Too Many Requests is recoverable. Sometimes the server puts
		// a Retry-After response header to indicate when the server is
		// available to start processing request from client.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			return true, nil
		}

		// Check the response code. We retry on 500-range responses to allow
		// the server time to recover, as 500's are typically not permanent
		// errors and may relate to outages on the server side. This will catch
		// invalid response codes as well, like 0 and 999.
		if resp.StatusCode == 0 || (resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented) {
			return true, fmt.Errorf("unexpected HTTP status %s", resp.Status)
		}

		return false, nil
	}
	return rClient.StandardClient()
}