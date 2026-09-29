package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s/api/v4%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "allpipes/1.0")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", reqURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("gitlab unauthorized (401): please verify your GITLAB_TOKEN")
		case http.StatusForbidden:
			return nil, fmt.Errorf("gitlab forbidden (403): token may lack 'read_api' permission")
		case http.StatusNotFound:
			return nil, fmt.Errorf("gitlab resource not found (404) at %s", endpoint)
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("gitlab rate limited (429): please increase poll interval")
		default:
			return nil, fmt.Errorf("gitlab error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}

	return resp, nil
}

func (c *Client) GetGroup(ctx context.Context, groupIDOrPath string) (*Group, error) {
	escapedPath := url.PathEscape(groupIDOrPath)
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/groups/%s", escapedPath))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var grp Group
	if err := json.NewDecoder(resp.Body).Decode(&grp); err != nil {
		return nil, fmt.Errorf("failed to decode group json: %w", err)
	}
	return &grp, nil
}

func (c *Client) GetGroupProjects(ctx context.Context, groupIDOrPath string, includeSubgroups bool) ([]Project, error) {
	escapedPath := url.PathEscape(groupIDOrPath)
	var allProjects []Project
	page := 1
	perPage := 100

	for {
		endpoint := fmt.Sprintf("/groups/%s/projects?include_subgroups=%t&per_page=%d&page=%d&archived=false&simple=true",
			escapedPath, includeSubgroups, perPage, page)

		resp, err := c.doRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}

		var projects []Project
		err = json.NewDecoder(resp.Body).Decode(&projects)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode projects json: %w", err)
		}

		allProjects = append(allProjects, projects...)

		nextPageHeader := resp.Header.Get("X-Next-Page")
		if nextPageHeader == "" {
			break
		}
		nextPage, err := strconv.Atoi(nextPageHeader)
		if err != nil || nextPage <= page {
			break
		}
		page = nextPage

		if page > 20 {
			break
		}
	}

	return allProjects, nil
}

func (c *Client) GetProjectPipelines(ctx context.Context, projectID int64, perPage int) ([]Pipeline, error) {
	if perPage <= 0 {
		perPage = 3
	}
	endpoint := fmt.Sprintf("/projects/%d/pipelines?per_page=%d&order_by=updated_at&sort=desc", projectID, perPage)

	resp, err := c.doRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pipelines []Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&pipelines); err != nil {
		return nil, fmt.Errorf("failed to decode pipelines json: %w", err)
	}
	return pipelines, nil
}

func (c *Client) GetPipelineDetails(ctx context.Context, projectID, pipelineID int64) (*Pipeline, error) {
	endpoint := fmt.Sprintf("/projects/%d/pipelines/%d", projectID, pipelineID)

	resp, err := c.doRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var p Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("failed to decode pipeline details: %w", err)
	}
	return &p, nil
}
