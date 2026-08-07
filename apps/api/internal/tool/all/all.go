package all

import (
	"github.com/sayem314/oracle/apps/api/internal/tool"
	"github.com/sayem314/oracle/apps/api/internal/tool/calc"
	"github.com/sayem314/oracle/apps/api/internal/tool/datetime"
	"github.com/sayem314/oracle/apps/api/internal/tool/exec"
	"github.com/sayem314/oracle/apps/api/internal/tool/fs"
	"github.com/sayem314/oracle/apps/api/internal/tool/loop"
	"github.com/sayem314/oracle/apps/api/internal/tool/net"
)

// Groups returns the built-in tool group factories, matching oracle's default
// registration order. It is the single place new tool domains are wired in.
func Groups() []func() []tool.Tool {
	return []func() []tool.Tool{
		datetime.New,
		net.New,
		calc.New,
		fs.New,
		exec.New,
		loop.New,
	}
}
