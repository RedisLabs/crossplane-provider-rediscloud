package qatfplan

import (
	"context"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

// This wrapper only exposes the real store's current asynchronous operation
// to the existing test observer. All workspace work remains in Upjet's store.
type trackingNativeStore struct {
	inner   *terraform.WorkspaceStore
	current *terraform.Workspace
}

func newTrackingNativeStore() *trackingNativeStore {
	return &trackingNativeStore{inner: terraform.NewWorkspaceStore(logging.NewNopLogger())}
}

func (s *trackingNativeStore) Workspace(ctx context.Context, client resource.SecretClient, item resource.Terraformed, setup terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error) {
	ws, err := s.inner.Workspace(ctx, client, item, setup, cfg)
	if err == nil {
		s.current = ws
	}
	return ws, err
}
