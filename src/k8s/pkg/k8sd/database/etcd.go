package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/canonical/k8s/pkg/k8sd/features"
	"github.com/canonical/k8s/pkg/k8sd/types"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Etcd struct {
	kv clientv3.KV
	// prefix is the root prefix for all keys in etcd WITHOUT a trailing slash
	prefix  string
	nowFunc func() time.Time
}

var _ Database = (*Etcd)(nil)

var (
	clusterConfigPrefix       = "clusterConfig"
	featureStatusPrefix       = "featureStatus"
	kubernetesAuthTokenPrefix = "kubernetesAuthToken"
	workerTokenPrefix         = "workerToken"
)

func NewEtcd(c *clientv3.Client, prefix string) *Etcd {
	prefix = strings.TrimSuffix(prefix, "/")

	return &Etcd{
		kv:     clientv3.NewKV(c),
		prefix: prefix,
	}
}

func (e *Etcd) prepareKey(parts ...string) string {
	pp := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			pp = append(pp, strings.Trim(p, "/"))
		}
	}

	return strings.Join(append([]string{e.prefix}, pp...), "/")
}

// SetClusterAPIToken is not supported with etcd datastore.
func (e *Etcd) SetClusterAPIToken(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	key := e.prepareKey(clusterConfigPrefix, "token::capi")
	if _, err := e.kv.Put(ctx, key, token); err != nil {
		return fmt.Errorf("failed to set ClusterAPI token: %w", err)
	}

	return nil
}

// ValidateClusterAPIToken is not supported with etcd datastore.
func (e *Etcd) ValidateClusterAPIToken(ctx context.Context, token string) (bool, error) {
	key := e.prepareKey(clusterConfigPrefix, "token::capi")
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return false, fmt.Errorf("failed to get ClusterAPI token: %w", err)
	}

	return len(resp.Kvs) > 0, nil
}

// SetClusterConfig is not supported with etcd datastore.
func (e *Etcd) SetClusterConfig(ctx context.Context, new types.ClusterConfig) (types.ClusterConfig, error) {
	key := e.prepareKey(clusterConfigPrefix, "v1alpha2")
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to get cluster config: %w", err)
	}

	if len(resp.Kvs) == 0 {
		valueBytes, err := json.Marshal(new)
		if err != nil {
			return types.ClusterConfig{}, fmt.Errorf("failed to encode cluster config: %w", err)
		}

		if _, err := e.kv.Put(ctx, key, string(valueBytes)); err != nil {
			return types.ClusterConfig{}, fmt.Errorf("failed to set initial cluster config: %w", err)
		}
		return new, nil
	}

	final := new
	oldRev := int64(0)

	var old types.ClusterConfig
	if err := json.Unmarshal([]byte(resp.Kvs[0].Value), &old); err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to parse v1alpha2 config: %w", err)
	}

	oldRev = resp.Kvs[0].ModRevision

	final, err = types.MergeClusterConfig(old, new)
	if err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to merge new cluster configuration options: %w", err)
	}

	valuesBytes, err := json.Marshal(final)
	if err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to encode cluster config: %w", err)
	}

	txn, err := e.kv.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", oldRev)).
		Then(clientv3.OpPut(key, string(valuesBytes))).
		Commit()
	if err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to set cluster config: %w", err)
	}
	if !txn.Succeeded {
		return types.ClusterConfig{}, fmt.Errorf("cluster config was modified concurrently, please retry")
	}

	return final, nil
}

// GetClusterConfig is not supported with etcd datastore.
func (e *Etcd) GetClusterConfig(ctx context.Context) (types.ClusterConfig, error) {
	key := e.prepareKey(clusterConfigPrefix, "v1alpha2")
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to get cluster config: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return types.ClusterConfig{}, fmt.Errorf("no cluster config found")
	}

	var clusterConfig types.ClusterConfig
	if err := json.Unmarshal([]byte(resp.Kvs[0].Value), &clusterConfig); err != nil {
		return types.ClusterConfig{}, fmt.Errorf("failed to parse v1alpha2 config: %w", err)
	}

	return clusterConfig, nil
}

// SetFeatureStatus is not supported with etcd datastore.
func (e *Etcd) SetFeatureStatus(ctx context.Context, name types.FeatureName, status types.FeatureStatus) error {
	valueBytes, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to encode feature status: %w", err)
	}

	key := e.prepareKey(featureStatusPrefix, string(name))
	if _, err := e.kv.Put(ctx, key, string(valueBytes)); err != nil {
		return fmt.Errorf("failed to set feature status: %w", err)
	}

	return nil
}

// GetFeatureStatuses is not supported with etcd datastore.
func (e *Etcd) GetFeatureStatuses(ctx context.Context) (map[types.FeatureName]types.FeatureStatus, error) {
	statuses := make(map[types.FeatureName]types.FeatureStatus)

	key := e.prepareKey(featureStatusPrefix)
	resp, err := e.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to get feature statuses: %w", err)
	}

	for _, kv := range resp.Kvs {
		// key does not end in a slash so we trim key+"/"
		nameStr := strings.TrimPrefix(string(kv.Key), key+"/")
		name := types.FeatureName(nameStr)
		switch name {
		case features.Network, features.DNS, features.MetricsServer, features.Ingress,
			features.Gateway, features.LoadBalancer, features.LocalStorage:
		default:
			return nil, fmt.Errorf("unknown feature name: %s", name)
		}

		var fs types.FeatureStatus
		if err := json.Unmarshal(kv.Value, &fs); err != nil {
			return nil, fmt.Errorf("failed to parse feature status for %s: %w", name, err)
		}
		statuses[name] = fs
	}

	return statuses, nil
}

type AuthToken struct {
	Username string
	Groups   []string
	Token    string
}

// CheckToken is not supported with etcd datastore.
func (e *Etcd) CheckToken(ctx context.Context, token string) (string, []string, error) {
	key := e.prepareKey(kubernetesAuthTokenPrefix)
	resp, err := e.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return "", nil, fmt.Errorf("failed to get cluster config: %w", err)
	}

	for _, kv := range resp.Kvs {
		t := AuthToken{}
		if err := json.Unmarshal(kv.Value, &t); err != nil {
			return "", nil, fmt.Errorf("failed to unmarshal auth token: %w", err)
		}

		if t.Token == token {
			return t.Username, t.Groups, nil
		}
	}

	return "", nil, fmt.Errorf("invalid token")
}

// GetOrCreateToken is not supported with etcd datastore.
func (e *Etcd) GetOrCreateToken(ctx context.Context, username string, groups []string) (string, error) {
	key := e.prepareKey(kubernetesAuthTokenPrefix, username)
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return "", fmt.Errorf("failed to get auth token for user %s: %w", username, err)
	}

	if len(resp.Kvs) > 0 {
		var t AuthToken
		if err := json.Unmarshal(resp.Kvs[0].Value, &t); err != nil {
			return "", fmt.Errorf("failed to unmarshal existing auth token for user %s: %w", username, err)
		}
		return t.Token, nil
	}

	// generate random bytes for the token
	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("is the system entropy low? failed to get random bytes: %w", err)
	}
	token := fmt.Sprintf("token::%s", hex.EncodeToString(tokenBytes))

	t := AuthToken{
		Username: username,
		Groups:   groups,
		Token:    token,
	}

	valueBytes, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("failed to encode auth token: %w", err)
	}

	if _, err := e.kv.Put(ctx, key, string(valueBytes)); err != nil {
		return "", fmt.Errorf("failed to create auth token for user %s: %w", username, err)
	}

	return token, nil
}

// DeleteToken is not supported with etcd datastore.
func (e *Etcd) DeleteToken(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	key := e.prepareKey(kubernetesAuthTokenPrefix)
	resp, err := e.kv.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to get auth tokens: %w", err)
	}

	for _, kv := range resp.Kvs {
		t := AuthToken{}
		if err := json.Unmarshal(kv.Value, &t); err != nil {
			return fmt.Errorf("failed to unmarshal auth token: %w", err)
		}

		if t.Token == token {
			if _, err := e.kv.Delete(ctx, string(kv.Key)); err != nil {
				return fmt.Errorf("failed to delete auth token: %w", err)
			}
		}
	}

	return nil
}

type WorkerToken struct {
	Token  string
	Expire time.Time
}

// CheckWorkerNodeToken is not supported with etcd datastore.
func (e *Etcd) CheckWorkerNodeToken(ctx context.Context, nodeName string, token string) (bool, error) {
	key := e.prepareKey(workerTokenPrefix, nodeName)
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return false, fmt.Errorf("failed to get worker token for node %s: %w", nodeName, err)
	}

	if len(resp.Kvs) == 0 {
		return false, nil
	}

	var wt WorkerToken
	if err := json.Unmarshal(resp.Kvs[0].Value, &wt); err != nil {
		return false, fmt.Errorf("failed to parse worker token for node %s: %w", nodeName, err)
	}

	isValidToken := wt.Token == token
	notExpired := e.nowFunc().Before(wt.Expire)
	return isValidToken && notExpired, nil
}

// GetOrCreateWorkerNodeToken is not supported with etcd datastore.
func (e *Etcd) GetOrCreateWorkerNodeToken(ctx context.Context, nodeName string, expiry time.Time) (string, error) {
	key := e.prepareKey(workerTokenPrefix, nodeName)
	resp, err := e.kv.Get(ctx, key, clientv3.WithLimit(1))
	if err != nil {
		return "", fmt.Errorf("failed to get worker token for node %s: %w", nodeName, err)
	}

	if len(resp.Kvs) > 0 {
		var wt WorkerToken
		if err := json.Unmarshal(resp.Kvs[0].Value, &wt); err != nil {
			return "", fmt.Errorf("failed to parse existing worker token for node %s: %w", nodeName, err)
		}
		if e.nowFunc().Before(wt.Expire) {
			return wt.Token, nil
		}
	}

	// generate random bytes for the token
	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("is the system entropy low? failed to get random bytes: %w", err)
	}
	token := fmt.Sprintf("worker::%s", hex.EncodeToString(tokenBytes))

	wt := WorkerToken{
		Token:  token,
		Expire: expiry,
	}
	valueBytes, err := json.Marshal(wt)
	if err != nil {
		return "", fmt.Errorf("failed to encode worker token: %w", err)
	}

	if _, err := e.kv.Put(ctx, key, string(valueBytes)); err != nil {
		return "", fmt.Errorf("failed to set worker token for node %s: %w", nodeName, err)
	}

	return token, nil
}

// DeleteWorkerNodeToken is not supported with etcd datastore.
func (e *Etcd) DeleteWorkerNodeToken(ctx context.Context, nodeName string) error {
	key := e.prepareKey(workerTokenPrefix, nodeName)
	if _, err := e.kv.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to delete worker token for node %s: %w", nodeName, err)
	}

	return nil
}
