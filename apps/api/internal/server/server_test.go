package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

func TestUnknownRoute(t *testing.T) {
	app, _, _ := newTestApp(t, llm.NewMock())

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/nope", nil))
	require.NoError(t, err)

	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}
