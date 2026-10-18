package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/johansundell/scribble/httperror"
)

func TestPong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	t.Run("Valid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/pong", bytes.NewBufferString(`{"key":"value"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		err := h.Pong(c)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		msg, ok := response["message"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected message to be a map, got %v", response["message"])
		}
		if msg["key"] != "value" {
			t.Errorf("Expected key 'value', got %v", msg["key"])
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/pong", bytes.NewBufferString(`{invalid json`))
		c.Request.Header.Set("Content-Type", "application/json")

		err := h.Pong(c)
		if err == nil {
			t.Fatal("Expected error for invalid JSON, got nil")
		}

		status := httperror.HTTPStatus(err)
		if status != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
		}
	})

	for name, body := range map[string]string{"null": `null`, "array": `[{"key":"value"}]`, "string": `"text"`, "number": `42`} {
		t.Run("Rejects "+name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/pong", bytes.NewBufferString(body))
			c.Request.Header.Set("Content-Type", "application/json")

			err := h.Pong(c)
			if err == nil || httperror.HTTPStatus(err) != http.StatusBadRequest {
				t.Fatalf("Expected 400 for %s, got err=%v status=%d", body, err, w.Code)
			}
		})
	}

	t.Run("Empty object", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/pong", bytes.NewBufferString(`{}`))
		c.Request.Header.Set("Content-Type", "application/json")

		if err := h.Pong(c); err != nil {
			t.Fatalf("Expected {} to be accepted, got %v", err)
		}
		if got := w.Body.String(); got != `{"message":{}}` {
			t.Errorf("Expected {\"message\":{}}, got %s", got)
		}
	})

	t.Run("Empty body", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/pong", bytes.NewBufferString(``))
		c.Request.Header.Set("Content-Type", "application/json")

		err := h.Pong(c)
		if err == nil {
			t.Fatal("Expected error for empty body, got nil")
		}

		status := httperror.HTTPStatus(err)
		if status != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
		}
	})
}
