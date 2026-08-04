package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sayem314/oracle/apps/api/internal/llm"
	"github.com/sayem314/oracle/apps/api/internal/tool"
)

func echoTool() tool.Tool {
	return tool.Tool{
		Definition: llm.Tool{Name: "echo", Description: "Echo the input back."},
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			return string(args), nil
		},
	}
}

func TestRegistryDefinitionsPreserveOrder(t *testing.T) {
	r := tool.NewRegistry()
	require.NoError(t, r.Register(echoTool()))
	require.NoError(t, r.Register(tool.Tool{Definition: llm.Tool{Name: "second"}}))

	defs := r.Definitions()
	require.Len(t, defs, 2)
	assert.Equal(t, "echo", defs[0].Name)
	assert.Equal(t, "second", defs[1].Name)
}

func TestRegistryExecute(t *testing.T) {
	r := tool.NewRegistry()
	require.NoError(t, r.Register(echoTool()))

	result, err := r.Execute(context.Background(), "echo", `{"msg":"hi"}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"msg":"hi"}`, result)
}

func TestRegistryExecuteUnknownTool(t *testing.T) {
	r := tool.NewRegistry()

	_, err := r.Execute(context.Background(), "missing", "{}")
	require.ErrorContains(t, err, `unknown tool "missing"`)
}

func TestRegistryExecuteInvalidJSON(t *testing.T) {
	r := tool.NewRegistry()
	require.NoError(t, r.Register(echoTool()))

	_, err := r.Execute(context.Background(), "echo", `{not json`)
	require.ErrorContains(t, err, "invalid JSON arguments")
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	r := tool.NewRegistry()
	require.NoError(t, r.Register(echoTool()))
	require.ErrorContains(t, r.Register(echoTool()), `duplicate tool "echo"`)
}

func TestRegistryRegisterEmptyName(t *testing.T) {
	r := tool.NewRegistry()
	require.ErrorContains(t, r.Register(tool.Tool{}), "missing a name")
}

func TestBuiltinToolsRegisterCleanly(t *testing.T) {
	r := tool.NewRegistry()
	for _, tl := range tool.NewBuiltin() {
		require.NoError(t, r.Register(tl))
	}
	assert.NotEmpty(t, r.Definitions())
}
