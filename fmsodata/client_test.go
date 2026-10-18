package fmsodata

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetRecords(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Basic dXNlcjpwYXNz", r.Header.Get("Authorization"))
		assert.Equal(t, "/fmi/odata/v4/testdb/Table1", r.URL.Path)
		assert.Equal(t, "filter=field%20eq%20%27value%27", r.URL.RawQuery)

		response := ODataResponse{
			Value: []map[string]interface{}{
				{"ID": "1", "Name": "Test"},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL, // Use server URL as host, but NewClient appends /fmi/odata...
		Database: "testdb",
		Username: "user",
		Password: "pass",
		Timeout:  10 * time.Second,
	})
	// Hack to fix the base URL for the mock server since NewClient appends the path
	// In a real scenario, Host would be just the domain
	// Here server.URL includes http://ip:port
	// We need to adjust the baseURL in the client to match the mock server's expectation if we want to test exact paths
	// But NewClient does: fmt.Sprintf("%s/fmi/odata/v4/%s", config.Host, config.Database)
	// So if Host is http://ip:port, baseURL is http://ip:port/fmi/odata/v4/testdb
	// The mock server will receive requests at /fmi/odata/v4/testdb/...
	// So this is correct.

	query := url.Values{}
	query.Set("filter", "field eq 'value'")

	records, err := client.GetRecords(context.Background(), "Table1", query)
	assert.NoError(t, err)
	assert.Len(t, records, 1)
	assert.Equal(t, "Test", records[0]["Name"])
}

func TestGetRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/fmi/odata/v4/testdb/Table1('1')", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"ID": "1", "Name": "Test"})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	record, err := client.GetRecord(context.Background(), "Table1", "1")
	assert.NoError(t, err)
	assert.Equal(t, "Test", record["Name"])
}

func TestCreateRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/fmi/odata/v4/testdb/Table1", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	_, err := client.CreateRecord(context.Background(), "Table1", map[string]interface{}{"Name": "New"})
	assert.NoError(t, err)
}

func TestRunScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/fmi/odata/v4/testdb/Script.TestScript", r.URL.Path)

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "param", body["scriptParameterValue"])

		response := ScriptResponse{
			ScriptResult: ScriptResult{
				Code:            0,
				ResultParameter: "Success",
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	result, err := client.RunScript(context.Background(), "TestScript", "param")
	assert.NoError(t, err)
	assert.Equal(t, 0, result.Code)
	assert.Equal(t, "Success", result.ResultParameter)
}

func TestUploadContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PATCH", r.Method)
		assert.Equal(t, "/fmi/odata/v4/testdb/Table1('1')", r.URL.Path)

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "dGVzdA==", body["ContainerField"]) // Base64 for "test"

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	err := client.UploadContainer(context.Background(), "Table1", "1", "ContainerField", bytes.NewBufferString("test"))
	assert.NoError(t, err)
}

func TestDownloadContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/fmi/odata/v4/testdb/Table1('1')/ContainerField/$value", r.URL.Path)

		w.Write([]byte("test data"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	data, err := client.DownloadContainer(context.Background(), "Table1", "1", "ContainerField")
	assert.NoError(t, err)
	assert.Equal(t, []byte("test data"), data)
}

func TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/fmi/odata/v4/testdb", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Host:     server.URL,
		Database: "testdb",
		Username: "user",
		Password: "pass",
	})

	err := client.Ping(context.Background())
	assert.NoError(t, err)
}

func newTestClient(serverURL string) *Client {
	return NewClient(ClientConfig{Host: serverURL, Database: "testdb", Username: "user", Password: "pass", Timeout: 10 * time.Second})
}

func TestGetRecords_FollowsNextLink(t *testing.T) {
	var requests []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		assert.Equal(t, "Basic dXNlcjpwYXNz", r.Header.Get("Authorization"))
		switch r.URL.Query().Get("$skip") {
		case "":
			// Relative nextLink, as FileMaker may return
			json.NewEncoder(w).Encode(map[string]interface{}{
				"value":           []map[string]interface{}{{"ID": 1}, {"ID": 2}},
				"@odata.nextLink": "Logs?$top=2&$skip=2",
			})
		case "2":
			// Absolute nextLink on the same server
			json.NewEncoder(w).Encode(map[string]interface{}{
				"value":           []map[string]interface{}{{"ID": 3}, {"ID": 4}},
				"@odata.nextLink": server.URL + "/fmi/odata/v4/testdb/Logs?$top=2&$skip=4",
			})
		case "4":
			json.NewEncoder(w).Encode(map[string]interface{}{"value": []map[string]interface{}{{"ID": 5}}})
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()

	query := url.Values{}
	query.Set("$top", "2")
	records, err := newTestClient(server.URL).GetRecords(context.Background(), "Logs", query)
	assert.NoError(t, err)
	assert.Len(t, records, 5)
	assert.Equal(t, float64(5), records[4]["ID"])
	assert.Len(t, requests, 3)
}

func TestGetRecords_RejectsNextLinkToOtherHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("credentials must not be sent to another host, got request with Authorization %q", r.Header.Get("Authorization"))
	}))
	defer other.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"value":           []map[string]interface{}{{"ID": 1}},
			"@odata.nextLink": other.URL + "/steal",
		})
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).GetRecords(context.Background(), "Logs", url.Values{})
	assert.ErrorContains(t, err, "points outside")
}

func TestGetRecords_DetectsPagingLoop(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"value":           []map[string]interface{}{{"ID": calls}},
			"@odata.nextLink": "Logs",
		})
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).GetRecords(context.Background(), "Logs", url.Values{})
	assert.ErrorContains(t, err, "paging loop")
	assert.Equal(t, 1, calls)
}

func TestGetRecords_FilterWithPlusOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The '+' in the offset must arrive as '+', and spaces as spaces.
		assert.Equal(t, "CreatedAt ge 2026-09-30T00:00:00+02:00", r.URL.Query().Get("$filter"))
		assert.NotContains(t, r.URL.RawQuery, "+")
		json.NewEncoder(w).Encode(map[string]interface{}{"value": []map[string]interface{}{}})
	}))
	defer server.Close()

	query := url.Values{}
	query.Set("$filter", "CreatedAt ge 2026-09-30T00:00:00+02:00")
	_, err := newTestClient(server.URL).GetRecords(context.Background(), "Logs", query)
	assert.NoError(t, err)
}

func TestCreateRecord_NoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	record, err := newTestClient(server.URL).CreateRecord(context.Background(), "Logs", map[string]interface{}{"Status": 200})
	assert.NoError(t, err)
	assert.Nil(t, record)
}

func TestCreateRecord_ReturnsCreatedRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"ID": 42, "Status": 200})
	}))
	defer server.Close()

	record, err := newTestClient(server.URL).CreateRecord(context.Background(), "Logs", map[string]interface{}{"Status": 200})
	assert.NoError(t, err)
	assert.Equal(t, float64(42), record["ID"])
}

func TestCreateRecord_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"-1","message":"bad field"}}`, http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).CreateRecord(context.Background(), "Logs", map[string]interface{}{"Nope": 1})
	assert.ErrorContains(t, err, "status 400")
}

func TestCreateRecords_BatchResponses(t *testing.T) {
	respond := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/fmi/odata/v4/testdb/$batch", r.URL.Path)
			assert.Contains(t, r.Header.Get("Content-Type"), "multipart/mixed; boundary=batch_")
			w.Header().Set("Content-Type", "multipart/mixed; boundary=b")
			w.Write([]byte(body))
		}))
	}
	records := []map[string]interface{}{{"Status": 200}, {"Status": 201}}

	t.Run("responses per create without closing blank line", func(t *testing.T) {
		srv := respond("--b\r\nContent-Type: multipart/mixed; boundary=c\r\n\r\n" +
			"--c\r\nContent-Type: application/http\r\n\r\nHTTP/1.1 204 No Content\r\n" +
			"--c\r\nContent-Type: application/http\r\n\r\nHTTP/1.1 201 Created\r\nContent-Length: 2\r\n\r\n{}\r\n" +
			"--c--\r\n--b--\r\n")
		defer srv.Close()
		assert.NoError(t, newTestClient(srv.URL).CreateRecords(context.Background(), "Logs", records))
	})

	t.Run("single error response for the change set", func(t *testing.T) {
		srv := respond("--b\r\nContent-Type: application/http\r\n\r\n" +
			"HTTP/1.1 400 Bad Request\r\nContent-Type: application/json\r\nContent-Length: 17\r\n\r\n{\"error\":\"nope\"}\n\r\n" +
			"--b--\r\n")
		defer srv.Close()
		err := newTestClient(srv.URL).CreateRecords(context.Background(), "Logs", records)
		var se *StatusError
		assert.ErrorAs(t, err, &se)
		assert.Equal(t, 400, se.StatusCode)
	})

	t.Run("failed create inside the change set", func(t *testing.T) {
		srv := respond("--b\r\nContent-Type: multipart/mixed; boundary=c\r\n\r\n" +
			"--c\r\nContent-Type: application/http\r\n\r\nHTTP/1.1 204 No Content\r\n\r\n\r\n" +
			"--c\r\nContent-Type: application/http\r\n\r\nHTTP/1.1 500 Internal Server Error\r\n\r\n\r\n" +
			"--c--\r\n--b--\r\n")
		defer srv.Close()
		err := newTestClient(srv.URL).CreateRecords(context.Background(), "Logs", records)
		var se *StatusError
		assert.ErrorAs(t, err, &se)
		assert.Equal(t, 500, se.StatusCode)
	})

	t.Run("whole batch rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}))
		defer srv.Close()
		err := newTestClient(srv.URL).CreateRecords(context.Background(), "Logs", records)
		var se *StatusError
		assert.ErrorAs(t, err, &se)
		assert.Equal(t, 401, se.StatusCode)
	})

	t.Run("empty batch sends nothing", func(t *testing.T) {
		assert.NoError(t, newTestClient("http://127.0.0.1:1").CreateRecords(context.Background(), "Logs", nil))
	})
}
