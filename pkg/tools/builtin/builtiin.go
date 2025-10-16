package builtin

import "github.com/docker/cagent/pkg/tools"

type elicitationTool struct{}

func (t *elicitationTool) SetElicitationHandler(tools.ElicitationHandler) {
	// No-op, this tool does not use elicitation
}

func (t *elicitationTool) SetOAuthSuccessHandler(func()) {
	// No-op, this tool does not use OAuth
}

func (t *elicitationTool) SetStreamOutputHandler(tools.StreamOutputHandler) {
	// No-op by default, override in tools that need streaming
}
