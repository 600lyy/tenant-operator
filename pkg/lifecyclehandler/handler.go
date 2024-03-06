package lifecyclehandler

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Lifecyclehandler struct {
	client.Client
}

func NewLifecycleHandler(c client.Client) Lifecyclehandler {
	return Lifecyclehandler{
		Client: c,
	}
}

func (r *Lifecyclehandler) updateStatus(ctx context.Context, obj client.Object) error {
	if err := r.Status().Update(ctx, obj); err != nil {
		return err
	}
	return nil
}
