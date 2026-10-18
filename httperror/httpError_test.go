package httperror

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestWrappedHTTPErrorPreservesStatus(t *testing.T) {
	err := fmt.Errorf("load user: %w", ReturnWithHTTPStatus(errors.New("missing"), http.StatusNotFound))

	if got := HTTPStatus(err); got != http.StatusNotFound {
		t.Fatalf("HTTPStatus() = %d, want %d", got, http.StatusNotFound)
	}
	if got := StatusText(err); got != http.StatusText(http.StatusNotFound) {
		t.Fatalf("StatusText() = %q, want %q", got, http.StatusText(http.StatusNotFound))
	}
}

func TestHTTPErrorDefaultsToInternalServerError(t *testing.T) {
	err := errors.New("unexpected failure")

	if got := HTTPStatus(err); got != http.StatusInternalServerError {
		t.Fatalf("HTTPStatus() = %d, want %d", got, http.StatusInternalServerError)
	}
	if got := StatusText(err); got != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("StatusText() = %q, want %q", got, http.StatusText(http.StatusInternalServerError))
	}
}
