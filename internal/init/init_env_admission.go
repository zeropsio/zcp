package init

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
)

// EnvAdmission makes the project's envIsolation admit the container host
// (ops.AdmitContainerToProjectEnv with the command layer's API client and
// project). The command layer builds it — init grows no API plumbing — and
// hands it in with WithEnvAdmission.
type EnvAdmission func(ctx context.Context, host string) (ops.EnvIsolationAdmission, error)

// WithEnvAdmission hands a Mate's container start the admission it asks once.
// Without one the step does nothing.
func WithEnvAdmission(admit EnvAdmission) Option {
	return func(o *options) { o.envAdmission = admit }
}

// envAdmissionTimeout bounds the one read and the one write: an API that does
// not answer must not hold the container's start.
const envAdmissionTimeout = 20 * time.Second

// envAdmissionStep is a Mate's container making itself a reader of its
// project's service variables at every start, a start after a zcp update
// included (docs/spec-mate.md C-14): one read of the project's envIsolation
// and, when it does not admit this container, one write that adds the
// container and keeps every other rule. It prints the one change it made;
// a refusal fails the step, which init reports once and carries on from.
func (o options) envAdmissionStep(_ string, rt runtime.Info) error {
	if o.envAdmission == nil || rt.ServiceName == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), envAdmissionTimeout)
	defer cancel()
	got, err := o.envAdmission(ctx, rt.ServiceName)
	if err != nil {
		return err
	}
	if got.Changed {
		fmt.Fprintf(os.Stderr, "    → project envIsolation %q → %q: %s now reads every service's variables that inherit it\n", got.Before, got.After, rt.ServiceName)
	}
	return nil
}
