package http

import (
	"fmt"
	"net/http"
)

type HTTPError struct {
	Response *http.Response
}

func NewHTTPError(r *http.Response) *HTTPError {
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}

	return &HTTPError{r}
}

func (e *HTTPError) StatusCode() int {
	return e.Response.StatusCode
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Error requesting %q status code: %d",
		e.Response.Request.URL, e.Response.StatusCode,
	)
}
