package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
)

type settingsResponse struct {
	PermissionDefault string `json:"permission_default"`
	PermissionRules   string `json:"permission_rules"`
	Instructions      string `json:"instructions"`
}

func TestSettingsRoundTrip(t *testing.T) {
	app, _, dbConn := newTestApp(t, llm.NewMock())
	cookie, _ := signUp(t, app, dbConn, "admin@example.com")

	res := doJobsRequest(t, app, http.MethodPut, "/api/v1/settings", cookie, map[string]any{
		"permission_default": "ask",
		"permission_rules":   "exec:allow",
		"instructions":       "Answer in haiku.",
	})
	require.Equal(t, http.StatusOK, res.StatusCode)

	var updated settingsResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&updated))
	assert.Equal(t, "ask", updated.PermissionDefault)
	assert.Equal(t, "exec:allow", updated.PermissionRules)
	assert.Equal(t, "Answer in haiku.", updated.Instructions)

	res = doJobsRequest(t, app, http.MethodGet, "/api/v1/settings", cookie, nil)
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got settingsResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, "Answer in haiku.", got.Instructions)
}
