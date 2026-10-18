package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPing(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	h := &Handler{} // Dependencies not needed for Ping

	t.Run("Valid argument", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Mock param
		c.Params = gin.Params{{Key: "argument", Value: "pong"}}

		err := h.Ping(c)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
		}

		var response map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}
		if response["result"] != "pong" {
			t.Errorf("Expected result 'pong', got '%s'", response["result"])
		}
	})

	t.Run("Not found argument", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		// Mock param
		c.Params = gin.Params{{Key: "argument", Value: "notfound"}}

		err := h.Ping(c)

		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}
