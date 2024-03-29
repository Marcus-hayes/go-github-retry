package retry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type GithubTestSuite struct {
	suite.Suite
	testGithubClient *github.Client
}

func (suite *GithubTestSuite) SetupTest() {
	defaultMinBackoff = 250 * time.Millisecond
	defaultMaxBackoff = 1 * time.Second
}

func (suite *GithubTestSuite) TestBackoffOnRequestErrs() {
	suite.configureTestServer(false, 100, []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), err, nil)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubTestSuite) TestBackoffOnMaxRequestErrs() {
	suite.configureTestServer(false, 100, []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusForbidden, http.StatusForbidden})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), resp)
	assert.ErrorContains(suite.T(), err, "giving up after 4 attempt(s)")
}

func (suite *GithubTestSuite) TestBackoffOnRequestErrWithRetryAfterHeader() {
	suite.configureTestServer(true, 100, []int{http.StatusTooManyRequests, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), err, nil)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubTestSuite) TestBackoffOnRateLimitReset() {
	suite.configureTestServer(false, 0, []int{http.StatusTooManyRequests, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), err, nil)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubTestSuite) TestBackoffOnRequestErrWithDefaultBackoff() {
	suite.configureTestServer(false, 100, []int{http.StatusTooManyRequests, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubTestSuite) TestCheckRetryWithBadContext() {
	suite.configureTestServer(false, 100, []int{http.StatusTooManyRequests, http.StatusOK})
	ctx, cancelFunc := context.WithCancel(context.Background())
	cancelFunc()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.ErrorIs(suite.T(), err, context.Canceled)
	assert.Nil(suite.T(), resp)
}

func (suite *GithubTestSuite) TestCheckRetryWithInternalServerError() {
	suite.configureTestServer(false, 100, []int{http.StatusInternalServerError, http.StatusOK})
	ctx := context.Background()
	_, resp, err := suite.testGithubClient.Users.Get(ctx, "myuser")
	assert.Nil(suite.T(), err)
	assert.Equal(suite.T(), resp.StatusCode, http.StatusOK)
}

func (suite *GithubTestSuite) configureTestServer(hasRetryAfter bool, remainingRate int, respCodeSlc []int) {
	mux := http.NewServeMux()
	i := 0
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if hasRetryAfter {
			w.Header().Set("Retry-After", "0.5")
		}
		w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(remainingRate))
		w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", time.Now().Unix()))
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

func TestGithubBackoffTestSuite(t *testing.T) {
	suite.Run(t, new(GithubTestSuite))
}
