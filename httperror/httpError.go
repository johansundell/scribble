package httperror

import (
	"errors"
	"fmt"
	"net/http"
)

type statusError struct {
	error
	status int
}

func (e statusError) Unwrap() error {
	return e.error
}

func HTTPStatus(err error) int {
	var statusErr statusError
	if errors.As(err, &statusErr) {
		return statusErr.status
	}
	return http.StatusInternalServerError
}

func StatusText(err error) string {
	var statusErr statusError
	if errors.As(err, &statusErr) {
		return http.StatusText(statusErr.status)
	}
	return http.StatusText(http.StatusInternalServerError)
}

func (se statusError) Error() string {
	return fmt.Sprintf("status %d: err %v", se.status, se.error)
}

func ReturnWithHTTPStatus(err error, status int) error {
	return statusError{
		error:  err,
		status: status,
	}
}
