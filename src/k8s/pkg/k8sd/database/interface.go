package database

import (
	"context"
	"time"

	"github.com/canonical/k8s/pkg/k8sd/types"
)

type Database interface {
	SetClusterAPIToken(ctx context.Context, token string) error
	ValidateClusterAPIToken(ctx context.Context, token string) (bool, error)

	SetClusterConfig(ctx context.Context, new types.ClusterConfig) (types.ClusterConfig, error)
	GetClusterConfig(ctx context.Context) (types.ClusterConfig, error)

	SetFeatureStatus(ctx context.Context, name types.FeatureName, status types.FeatureStatus) error
	GetFeatureStatuses(ctx context.Context) (map[types.FeatureName]types.FeatureStatus, error)

	CheckToken(ctx context.Context, token string) (string, []string, error)
	GetOrCreateToken(ctx context.Context, username string, groups []string) (string, error)
	DeleteToken(ctx context.Context, token string) error

	CheckWorkerNodeToken(ctx context.Context, nodeName string, token string) (bool, error)
	GetOrCreateWorkerNodeToken(ctx context.Context, nodeName string, expiry time.Time) (string, error)
	DeleteWorkerNodeToken(ctx context.Context, nodeName string) error
}
