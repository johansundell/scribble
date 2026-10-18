package fmsodata

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// UploadContainer uploads data to a container field
func (c *Client) UploadContainer(ctx context.Context, tableName string, id string, fieldName string, data io.Reader) error {
	content, err := io.ReadAll(data)
	if err != nil {
		return err
	}

	// OData takes container data as Base64 in the JSON body
	payload := map[string]interface{}{
		fieldName: base64.StdEncoding.EncodeToString(content),
	}
	resp, err := c.sendRequest(ctx, http.MethodPatch, fmt.Sprintf("%s/%s('%s')", c.baseURL, tableName, id), payload)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// DownloadContainer downloads data from a container field
func (c *Client) DownloadContainer(ctx context.Context, tableName string, id string, fieldName string) ([]byte, error) {
	resp, err := c.sendRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%s('%s')/%s/$value", c.baseURL, tableName, id, fieldName), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
