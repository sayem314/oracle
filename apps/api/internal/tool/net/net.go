package net

import "github.com/sayem314/oracle/apps/api/internal/tool"

// New returns the network tools, sharing one plain http.Client.
func New() []tool.Tool {
	hc := httpClient()
	return []tool.Tool{
		webFetchTool(hc),
		webSearchTool(hc),
		httpRequestTool(hc),
		weatherTool(hc),
	}
}
