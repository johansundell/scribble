package fmsodata

import (
	"crypto/tls"
	"time"
)

// ODataResponse represents a generic OData response
type ODataResponse struct {
	Context string                   `json:"@odata.context,omitempty"`
	Count   int                      `json:"@odata.count,omitempty"`
	Value   []map[string]interface{} `json:"value,omitempty"`
	// NextLink points to the next page; FileMaker returns at most 10,000
	// records per response.
	NextLink string `json:"@odata.nextLink,omitempty"`
}

// ClientConfig holds configuration for the OData client
type ClientConfig struct {
	Host     string
	Database string
	Username string
	Password string
	Timeout  time.Duration
	// TLSConfig, when set, is used for HTTPS connections, for example to trust
	// a private CA. Nil uses Go's default verification.
	TLSConfig *tls.Config
}

// ScriptResult represents the result of a script execution
type ScriptResult struct {
	Code            int         `json:"code"`
	ResultParameter interface{} `json:"resultParameter"`
}

// ScriptResponse represents the response from a script execution
type ScriptResponse struct {
	ScriptResult ScriptResult `json:"scriptResult"`
}

// FieldDefinition represents a field definition for creating a table
type FieldDefinition struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Primary bool        `json:"primary,omitempty"`
	Unique  bool        `json:"unique,omitempty"`
	Global  bool        `json:"global,omitempty"`
	Default interface{} `json:"default,omitempty"`
}

// TableDefinition represents a table definition for creating a table
type TableDefinition struct {
	TableName string            `json:"tableName"`
	Fields    []FieldDefinition `json:"fields"`
}
