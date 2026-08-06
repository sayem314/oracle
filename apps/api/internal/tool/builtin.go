package tool

// NewBuiltin returns the tools oracle ships with.
func NewBuiltin() []Tool {
	return []Tool{
		getTimeTool(),
		dateCalcTool(),
		timezoneConvertTool(),
		webFetchTool(httpClient()),
		webSearchTool(httpClient()),
		weatherTool(httpClient()),
		mathEvalTool(),
	}
}
