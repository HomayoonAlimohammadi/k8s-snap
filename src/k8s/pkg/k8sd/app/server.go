package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	apiv1 "github.com/canonical/k8s-snap-api/api/v1"
	"github.com/canonical/k8s/pkg/k8sd/database"
	databaseutil "github.com/canonical/k8s/pkg/k8sd/database/util"
	"github.com/canonical/k8s/pkg/k8sd/features"
	"github.com/canonical/k8s/pkg/k8sd/types"
	"github.com/canonical/lxd/lxd/response"
)

type Server struct {
	srv *http.Server
}

func NewServer(a App) *Server {
	mux := http.NewServeMux()
	addRoutes(mux, a)
	s := &Server{}
	s.srv = &http.Server{}

	return s
}

func addRoutes(mux *http.ServeMux, a *App) {
	mux.HandleFunc("GET /1.0/k8sd/cluster", a.getClusterStatus)

}

func (a *App) getClusterStatus(w http.ResponseWriter, r *http.Request) {
	config, err := databaseutil.GetClusterConfig(r.Context(), s)
	if err != nil {
		return response.InternalError(fmt.Errorf("failed to get cluster config: %w", err))
	}

	client, err := e.provider.Snap().KubernetesClient("")
	if err != nil {
		return response.InternalError(fmt.Errorf("failed to create k8s client: %w", err))
	}

	ready, err := client.HasReadyNodes(r.Context())
	if err != nil {
		return response.InternalError(fmt.Errorf("failed to check if cluster has ready nodes: %w", err))
	}

	var statuses map[types.FeatureName]types.FeatureStatus
	if err := s.Database().Transaction(r.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var err error
		statuses, err = database.GetFeatureStatuses(r.Context(), tx)
		if err != nil {
			return fmt.Errorf("failed to get feature statuses: %w", err)
		}
		return nil
	}); err != nil {
		return response.InternalError(fmt.Errorf("database transaction failed: %w", err))
	}

	return response.SyncResponse(true, &apiv1.ClusterStatusResponse{
		ClusterStatus: apiv1.ClusterStatus{
			Ready:   ready,
			Members: members,
			Config:  config.ToUserFacing(),
			Datastore: apiv1.Datastore{
				Type:    config.Datastore.GetType(),
				Servers: config.Datastore.GetExternalServers(),
			},
			DNS:           statuses[features.DNS].ToAPI(),
			Network:       statuses[features.Network].ToAPI(),
			LoadBalancer:  statuses[features.LoadBalancer].ToAPI(),
			Ingress:       statuses[features.Ingress].ToAPI(),
			Gateway:       statuses[features.Gateway].ToAPI(),
			MetricsServer: statuses[features.MetricsServer].ToAPI(),
			LocalStorage:  statuses[features.LocalStorage].ToAPI(),
		},
	})
}
