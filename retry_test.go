package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type GithubRetryTestSuite struct {
	suite.Suite
	testGithubClient *github.Client
}

func (suite *GithubRetryTestSuite) SetupTest() {
	DefaultMinBackoff = 250 * time.Millisecond
	DefaultMaxBackoff = 1 * time.Second
}

func (suite *GithubRetryTestSuite) TestRetryOnRequestErrs() {
	suite.configureTestServer(false, []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.ErrorIs(suite.T(), err, nil)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubRetryTestSuite) TestRetryOnMaxRequestErrs() {
	suite.configureTestServer(false, []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusForbidden, http.StatusForbidden})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), resp)
	assert.ErrorContains(suite.T(), err, "giving up after 4 attempt(s)")
}

func TestGithubRetryTestSuite(t *testing.T) {
	suite.Run(t, new(GithubRetryTestSuite))
}

func (suite *GithubRetryTestSuite) configureTestServer(hasRetryAfter bool, respCodeSlc []int) {
	mux := http.NewServeMux()
	i := 0
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "100")
		w.WriteHeader(respCodeSlc[i])
		i += 1
	})
	svr := httptest.NewServer(mux)
	rc := NewClient()
	c := github.NewClient(rc)
	c.BaseURL = &url.URL{
		Scheme: "http",
		Host:   strings.Split(svr.URL, "://")[1],
		Path:   "/",
	}
	suite.testGithubClient = c
}
