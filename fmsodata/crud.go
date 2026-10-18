package fmsodata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GetRecords retrieves records from a table, following @odata.nextLink until
// all pages have been read.
func (c *Client) GetRecords(ctx context.Context, tableName string, query url.Values) ([]map[string]interface{}, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s", c.baseURL, tableName))
	if err != nil {
		return nil, err
	}
	u.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")

	var records []map[string]interface{}
	seen := map[string]bool{}
	for {
		page, err := c.getPage(ctx, u.String())
		if err != nil {
			return nil, err
		}
		records = append(records, page.Value...)
		if page.NextLink == "" {
			return records, nil
		}

		seen[u.String()] = true
		next, err := c.resolveNextLink(u, page.NextLink)
		if err != nil {
			return nil, err
		}
		if seen[next.String()] {
			return nil, fmt.Errorf("OData paging loop: %s was already requested", next)
		}
		u = next
	}
}

func (c *Client) getPage(ctx context.Context, pageURL string) (ODataResponse, error) {
	var page ODataResponse
	resp, err := c.sendRequest(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return page, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&page)
	return page, err
}

// resolveNextLink resolves a (possibly relative) nextLink against the current
// page URL. It must stay on the configured server: every request carries the
// Basic auth credentials.
func (c *Client) resolveNextLink(current *url.URL, link string) (*url.URL, error) {
	ref, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("invalid OData nextLink %q: %w", link, err)
	}
	next := current.ResolveReference(ref)
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	if next.Scheme != base.Scheme || next.Host != base.Host {
		return nil, fmt.Errorf("OData nextLink %q points outside %s://%s", link, base.Scheme, base.Host)
	}
	return next, nil
}

// GetRecord retrieves a single record by ID
func (c *Client) GetRecord(ctx context.Context, tableName string, id string) (map[string]interface{}, error) {
	resp, err := c.sendRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%s('%s')", c.baseURL, tableName, id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var record map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&record); err != nil {
		return nil, err
	}
	return record, nil
}

// CreateRecord creates a new record. It returns the created record, or nil
// when the server answers 204 No Content (for example when the request asked
// for Prefer: return=minimal).
func (c *Client) CreateRecord(ctx context.Context, tableName string, data map[string]interface{}) (map[string]interface{}, error) {
	resp, err := c.sendRequest(ctx, http.MethodPost, fmt.Sprintf("%s/%s", c.baseURL, tableName), data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var record map[string]interface{}
	if resp.StatusCode != http.StatusNoContent && resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(&record); err != nil && err != io.EOF {
			return nil, err
		}
	}
	return record, nil
}

// UpdateRecord updates an existing record
func (c *Client) UpdateRecord(ctx context.Context, tableName string, id string, data map[string]interface{}) error {
	resp, err := c.sendRequest(ctx, http.MethodPatch, fmt.Sprintf("%s/%s('%s')", c.baseURL, tableName, id), data)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// DeleteRecord deletes a record
func (c *Client) DeleteRecord(ctx context.Context, tableName string, id string) error {
	resp, err := c.sendRequest(ctx, http.MethodDelete, fmt.Sprintf("%s/%s('%s')", c.baseURL, tableName, id), nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// StatusError is returned when the server answers with an error status.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("OData request failed with status %d: %s", e.StatusCode, e.Body)
}

// maxErrorBody caps how much of an error response is kept in a StatusError.
const maxErrorBody = 64 << 10

func (c *Client) handleError(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return &StatusError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
}
