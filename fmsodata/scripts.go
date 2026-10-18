package fmsodata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RunScript runs a script in the FileMaker database
func (c *Client) RunScript(ctx context.Context, scriptName string, parameter interface{}) (ScriptResult, error) {
	var payload interface{}
	if parameter != nil {
		payload = map[string]interface{}{"scriptParameterValue": parameter}
	}

	resp, err := c.sendRequest(ctx, http.MethodPost, fmt.Sprintf("%s/Script.%s", c.baseURL, scriptName), payload)
	if err != nil {
		return ScriptResult{}, err
	}
	defer resp.Body.Close()

	var scriptResp ScriptResponse
	if err := json.NewDecoder(resp.Body).Decode(&scriptResp); err != nil {
		return ScriptResult{}, err
	}
	return scriptResp.ScriptResult, nil
}
