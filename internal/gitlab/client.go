package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxProjectPages = 50

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Authenticated() bool { return c.token != "" }

type APIError struct {
	StatusCode int
	Path       string
	Detail     string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	var msg string
	switch e.StatusCode {
	case http.StatusUnauthorized:
		msg = "GitLab rejected the token (401)"
	case http.StatusForbidden:
		msg = "access denied (403) on " + e.Path
	case http.StatusNotFound:
		msg = "not found (404): " + e.Path
	case http.StatusTooManyRequests:
		msg = "rate limited by GitLab (429)"
	default:
		msg = fmt.Sprintf("GitLab returned %d for %s", e.StatusCode, e.Path)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

type NetworkError struct {
	Path string
	Err  error
}

func (e *NetworkError) Error() string {
	var urlErr *url.Error
	switch {
	case errors.As(e.Err, &urlErr) && urlErr.Timeout():
		return "GitLab did not respond in time to " + e.Path
	case errors.As(e.Err, &urlErr):
		return "could not reach GitLab: " + urlErr.Err.Error()
	}
	return e.Err.Error()
}

func (e *NetworkError) Unwrap() error { return e.Err }

func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	reqURL := c.baseURL + "/api/v4" + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "allpipes")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &NetworkError{Path: path, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		detail := strings.TrimPrefix(errorDetail(body), strconv.Itoa(resp.StatusCode)+" ")
		if detail == http.StatusText(resp.StatusCode) {
			detail = ""
		}
		apiErr := &APIError{StatusCode: resp.StatusCode, Path: path, Detail: detail}
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(secs) * time.Second
		}
		return nil, apiErr
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return resp.Header, nil
}

func errorDetail(body []byte) string {
	var payload struct {
		Message          any    `json:"message"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if payload.ErrorDescription != "" {
		return payload.ErrorDescription
	}
	if s, ok := payload.Message.(string); ok {
		return s
	}
	return payload.Error
}

func (c *Client) GetGroup(ctx context.Context, groupIDOrPath string) (*Group, error) {
	var group Group
	query := url.Values{"with_projects": {"false"}}
	if _, err := c.get(ctx, "/groups/"+url.PathEscape(groupIDOrPath), query, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func (c *Client) ListGroupProjects(ctx context.Context, groupIDOrPath string, includeSubgroups bool) ([]Project, error) {
	path := "/groups/" + url.PathEscape(groupIDOrPath) + "/projects"
	var all []Project

	for page := 1; page <= maxProjectPages; {
		query := url.Values{
			"include_subgroups": {strconv.FormatBool(includeSubgroups)},
			"with_shared":       {"false"},
			"archived":          {"false"},
			"simple":            {"true"},
			"per_page":          {"100"},
			"page":              {strconv.Itoa(page)},
		}
		var projects []Project
		header, err := c.get(ctx, path, query, &projects)
		if err != nil {
			return nil, err
		}
		all = append(all, projects...)

		next, err := strconv.Atoi(header.Get("X-Next-Page"))
		if err != nil || next <= page {
			break
		}
		page = next
	}
	return all, nil
}

func (c *Client) ListPipelines(ctx context.Context, projectID int64, limit int) ([]Pipeline, error) {
	// Keep GitLab's default id ordering: order_by=updated_at is unindexed and
	// times out with a 500 on projects with a long pipeline history.
	query := url.Values{"per_page": {strconv.Itoa(limit)}}
	var pipelines []Pipeline
	if _, err := c.get(ctx, fmt.Sprintf("/projects/%d/pipelines", projectID), query, &pipelines); err != nil {
		return nil, err
	}
	return pipelines, nil
}

// The list endpoint omits user, duration and timestamps; only the single pipeline has them.
func (c *Client) GetPipeline(ctx context.Context, projectID, pipelineID int64) (*Pipeline, error) {
	var p Pipeline
	if _, err := c.get(ctx, fmt.Sprintf("/projects/%d/pipelines/%d", projectID, pipelineID), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
