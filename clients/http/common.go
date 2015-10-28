package http

import (
	"fmt"
	"net/http"

	"gopkg.in/src-d/go-git.v2/clients/common"
)

type HTTPError struct {
	Response *http.Response
}

func NewHTTPError(r *http.Response) error {
	if r.StatusCode >= 200 && r.StatusCode < 300 {
		return nil
	}

	err := &HTTPError{r}
	if r.StatusCode == 404 || r.StatusCode == 401 {
		return common.NewPermanentError(common.NotFoundErr)
	}

	return common.NewUnexpectedError(err)
}

func (e *HTTPError) StatusCode() int {
	return e.Response.StatusCode
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected requesting %q status code: %d",
		e.Response.Request.URL, e.Response.StatusCode,
	)
}
