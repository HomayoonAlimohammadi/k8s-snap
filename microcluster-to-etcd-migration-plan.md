# Microcluster to Etcd Migration Plan

## Executive Summary

This document outlines the plan to remove microcluster dependency from k8sd and transition to a pure etcd-based clustering and data storage solution. The migration will eliminate dqlite dependency and simplify the architecture while maintaining all current functionality.

## Current State Analysis

### Current Microcluster Usage

Based on code analysis, microcluster is currently used for:

1. **Clustering Infrastructure**: Node discovery, membership management, and leader election
2. **HTTP API Server**: REST API endpoints for cluster operations 
3. **Database Storage**: Dqlite-based storage for:
   - Cluster configuration (`types.ClusterConfig`)
   - Feature status (`types.FeatureStatus`) 
   - Authentication tokens (Kubernetes auth tokens, worker tokens)
   - ClusterAPI tokens
4. **Node Lifecycle**: Bootstrap, join, remove operations
5. **State Management**: Database transactions and consistency guarantees

### Current Etcd Implementation Status

**✅ Already Implemented:**
- Etcd client wrapper (`pkg/client/etcd/client.go`)
- Etcd database implementation (`pkg/k8sd/database/etcd.go`) with full Database interface
- All database operations: cluster config, feature status, auth tokens, worker tokens
- Etcd setup and configuration (`pkg/k8sd/setup/etcd.go`)
- Etcd PKI management (`pkg/k8sd/pki/etcd.go`)

**⚠️ Partially Implemented:**
- HTTP server skeleton exists (`pkg/k8sd/app/server.go`) but incomplete
- App initialization connects to etcd but still depends on microcluster

## Migration Strategy

### Phase 1: HTTP Server Migration (High Priority)

**Objective**: Replace microcluster's HTTP server with a standalone implementation

**Tasks:**
1. **Complete HTTP Server Implementation**
   - Expand `pkg/k8sd/app/server.go` with all missing endpoints
   - Port all API endpoints from microcluster-based handlers to standalone handlers
   - Implement proper routing, middleware, and error handling

2. **API Endpoints to Migrate** (from `pkg/k8sd/api/`):
   - `POST /1.0/k8sd/cluster/bootstrap` (cluster_bootstrap.go)  
   - `POST /1.0/k8sd/cluster/join` (cluster_join.go)
   - `DELETE /1.0/k8sd/cluster/remove` (cluster_remove.go)
   - `GET /1.0/k8sd/cluster` (cluster.go)
   - `POST /1.0/k8sd/cluster/config` (cluster_config.go)
   - `GET /1.0/k8sd/cluster/tokens` (cluster_tokens.go)
   - `POST /1.0/k8sd/x/capi/*` (capi_*.go endpoints)
   - Worker node endpoints (worker.go)
   - Certificate management endpoints
   - All other existing endpoints in the api package

3. **Request/Response Handling**
   - Maintain compatibility with existing API contracts
   - Preserve all validation logic
   - Keep the same JSON request/response formats

### Phase 2: Clustering Logic Migration (High Priority)

**Objective**: Replace microcluster's clustering with etcd-based node management

**Tasks:**
1. **Node Discovery & Registration**
   - Implement etcd-based node registry using etcd keys like `/k8sd/nodes/{node-id}`
   - Store node metadata: address, role, status, join time
   - Replace microcluster member list with etcd-based node discovery

2. **Bootstrap Process**
   - Remove dependency on `microcluster.NewCluster()`
   - Implement direct etcd cluster initialization
   - Ensure first node creates etcd cluster and initializes schema

3. **Join Process** 
   - Replace `microcluster.JoinCluster()` with etcd member addition
   - Implement etcd member join protocol
   - Handle certificate distribution without microcluster

4. **Remove Process**
   - Replace microcluster node removal with etcd member removal
   - Clean up node-specific etcd keys
   - Handle etcd cluster member cleanup

### Phase 3: App Structure Refactoring (Medium Priority)

**Objective**: Remove microcluster dependencies from core App structure

**Tasks:**
1. **App Initialization (`pkg/k8sd/app/app.go`)**
   - Remove `microcluster.MicroCluster` field from App struct
   - Remove `client.Client` field  
   - Update `New()` function to not create microcluster instance
   - Replace microcluster hooks with direct function calls

2. **State Management**
   - Replace `state.State` parameter with etcd database interface
   - Update all hook functions to not depend on microcluster state
   - Remove `pkg/utils/microcluster/state.go` utility

3. **App Lifecycle**
   - Replace `app.Run()` microcluster startup with HTTP server startup
   - Implement graceful shutdown without microcluster
   - Update ready/health check logic

### Phase 4: Database Transition (Low Priority - Already Mostly Done)

**Objective**: Complete transition to etcd-only storage

**Tasks:**
1. **Database Interface Updates**
   - The etcd implementation is already complete in `pkg/k8sd/database/etcd.go`
   - Update all callers to use etcd database instead of dqlite transactions
   - Remove any remaining `sql.Tx` usage

2. **Schema Management**  
   - Remove dqlite schema definitions
   - Implement etcd-based schema versioning if needed
   - Handle data migration from existing dqlite databases (if needed)

### Phase 5: Testing & Integration (High Priority)

**Objective**: Ensure feature parity and reliability

**Tasks:**
1. **Unit Test Updates**
   - Update all tests that mock microcluster state
   - Create etcd-based test fixtures
   - Update `pkg/utils/microcluster/state.go` test utilities

2. **Integration Test Updates**  
   - Update `tests/integration/` to work without microcluster
   - Verify clustering operations (bootstrap, join, remove)
   - Test failure scenarios and recovery

3. **End-to-End Validation**
   - Multi-node cluster setup/teardown
   - Rolling upgrades  
   - Certificate management
   - Feature controller functionality

### Phase 6: Cleanup (Low Priority)

**Objective**: Remove all microcluster dependencies

**Tasks:**
1. **Code Cleanup**
   - Remove all microcluster imports
   - Delete unused microcluster utility code
   - Clean up configuration structures

2. **Dependency Management**
   - Remove microcluster from `go.mod`
   - Remove dqlite dependencies if no longer needed
   - Update build scripts and packaging

3. **Documentation Updates**
   - Update architecture documentation
   - Remove microcluster references from user guides
   - Update troubleshooting documentation

## Implementation Considerations

### Data Consistency
- Etcd provides strong consistency guarantees comparable to dqlite
- Use etcd transactions for atomic operations
- Implement retry logic for connection failures

### Security
- Maintain TLS security for etcd communication
- Preserve existing PKI management
- Ensure secure token handling

### Performance  
- Etcd may have different performance characteristics than dqlite
- Monitor memory and CPU usage during testing
- Consider connection pooling for high-load scenarios

### Backwards Compatibility
- Maintain API compatibility for existing clients
- Consider migration path for existing deployments
- Plan for rollback strategy if needed

## Risk Assessment

### High Risk
- **API Breaking Changes**: Ensure exact API compatibility to avoid breaking existing integrations
- **Data Loss**: Careful handling of existing cluster state during migration
- **Clustering Failures**: Robust testing of node join/leave operations

### Medium Risk  
- **Performance Degradation**: Monitor performance impacts of etcd vs dqlite
- **Certificate Management**: Ensure PKI operations continue working seamlessly

### Low Risk
- **Build Dependencies**: Straightforward removal of microcluster from build process
- **Documentation**: Updates are low-risk maintenance work

## Success Criteria

1. ✅ All existing API endpoints work identically
2. ✅ Multi-node clusters can be created, scaled, and managed
3. ✅ All integration tests pass
4. ✅ Performance is comparable to current implementation
5. ✅ No microcluster dependencies remain in codebase
6. ✅ Documentation accurately reflects new architecture

## Timeline Estimate

- **Phase 1 (HTTP Server)**: 2-3 weeks
- **Phase 2 (Clustering)**: 3-4 weeks  
- **Phase 3 (App Refactoring)**: 2-3 weeks
- **Phase 4 (Database)**: 1 week (mostly done)
- **Phase 5 (Testing)**: 2-3 weeks
- **Phase 6 (Cleanup)**: 1 week

**Total Estimated Timeline**: 11-17 weeks

## Next Steps

1. Start with Phase 1: Complete the HTTP server implementation
2. Focus on maintaining API compatibility throughout migration
3. Implement comprehensive testing at each phase
4. Plan for gradual rollout and monitoring of the new architecture

This migration will significantly simplify the k8sd architecture while maintaining all existing functionality and improving maintainability.