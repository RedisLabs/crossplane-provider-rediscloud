package qatfplan

import (
	"context"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

// The fake store retains a workspace like the existing fixture but delegates
// configuration generation to Upjet's actual FileProducer for each reconcile.
type regeneratingStore struct {
	dir string
	ws  *terraform.Workspace
}

func (s regeneratingStore) Workspace(ctx context.Context, client resource.SecretClient, item resource.Terraformed, setup terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error) {
	if s.ws.LastOperation.IsRunning() {
		return s.ws, nil
	}
	producer, err := terraform.NewFileProducer(ctx, client, s.dir, item, setup, cfg)
	if err != nil {
		return nil, err
	}
	if _, err = producer.WriteMainTF(); err != nil {
		return nil, err
	}
	return s.ws, nil
}
