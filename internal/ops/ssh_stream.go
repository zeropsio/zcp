package ops

import (
	"context"
	"io"

	"github.com/zeropsio/zcp/internal/platform"
)

// SSHStreamer is the side of an SSHDeployer that streams a remote command's
// stdout — and only its stdout — to a writer instead of buffering its combined
// output under ExecSSH's timeout: what carries a git bundle of a whole
// repository off a dev service. It is optional, so every SSHDeployer fake
// stays as it is; a caller that needs it asks with a type assertion and
// refuses without it. platform.SystemSSHDeployer implements it.
type SSHStreamer interface {
	StreamSSH(ctx context.Context, hostname, command string, stdout io.Writer) error
}

var _ SSHStreamer = (*platform.SystemSSHDeployer)(nil)
