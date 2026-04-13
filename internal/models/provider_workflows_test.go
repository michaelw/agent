package models

import (
	"context"
	"errors"
	"testing"

	"github.com/serverlessworkflow/sdk-go/v3/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workflowRequestConfigStub struct {
	identity    *Identity
	identityErr error
}

func (s *workflowRequestConfigStub) GetServices() ServicesClientImpl          { return nil }
func (s *workflowRequestConfigStub) GetEnvironment() EnvironmentConfig        { return EnvironmentConfig{} }
func (s *workflowRequestConfigStub) GetSecret() string                        { return "" }
func (s *workflowRequestConfigStub) GetLoginServerHostname() string           { return "" }
func (s *workflowRequestConfigStub) IsServer() bool                           { return false }
func (s *workflowRequestConfigStub) IsAgent() bool                            { return false }
func (s *workflowRequestConfigStub) IsClient() bool                           { return false }
func (s *workflowRequestConfigStub) GetServicesConfig() *ServicesConfig       { return nil }
func (s *workflowRequestConfigStub) GetEnvironmentConfig() *EnvironmentConfig { return nil }
func (s *workflowRequestConfigStub) GetResumeCallbackUrl(workflowTask *ElevateWorkflowTask) string {
	return ""
}
func (s *workflowRequestConfigStub) GetAuthCallbackUrl(providerName string) string { return "" }
func (s *workflowRequestConfigStub) GetSignalCallbackUrl(workflowTask *ElevateWorkflowTask) string {
	return ""
}
func (s *workflowRequestConfigStub) GetLoginServerUrl() string { return "" }
func (s *workflowRequestConfigStub) GetLocalServerUrl() string { return "" }
func (s *workflowRequestConfigStub) GetCompositeRole(identity *Identity, baseRole *Role, providers ...Provider) (*CompositeRole, error) {
	return &CompositeRole{Role: *baseRole}, nil
}
func (s *workflowRequestConfigStub) GetCompositeRoleForWorkflow(identity *Identity, baseRole *Role, workflowID string, providers ...Provider) (*CompositeRole, error) {
	return &CompositeRole{Role: *baseRole}, nil
}
func (s *workflowRequestConfigStub) GetIdentity(byEmail string) (*Identity, error) {
	if s.identityErr != nil {
		return nil, s.identityErr
	}
	if s.identity == nil {
		return nil, errors.New("identity not found")
	}
	return s.identity, nil
}
func (s *workflowRequestConfigStub) GetTenant(name string) (*ProviderTenant, error) {
	return nil, errors.New("tenant not found")
}
func (s *workflowRequestConfigStub) GetWorkflowByName(name string) (*Workflow, error) {
	return nil, errors.New("workflow not found")
}
func (s *workflowRequestConfigStub) GetWorkflowFromElevationRequest(elevationRequest *ElevateRequest) (*Workflow, error) {
	return nil, errors.New("workflow not found")
}
func (s *workflowRequestConfigStub) GetProviderByName(name string) (Provider, error) {
	return nil, errors.New("provider not found")
}
func (s *workflowRequestConfigStub) GetProvidersByCapability(capability ...ProviderCapability) map[string]Provider {
	return nil
}
func (s *workflowRequestConfigStub) GetProvidersByCapabilityWithUser(user *User, capability ...ProviderCapability) map[string]Provider {
	return nil
}

type workflowRequestProviderStub struct {
	*BaseProvider
}

func newWorkflowRequestProviderStub() *workflowRequestProviderStub {
	return &workflowRequestProviderStub{
		BaseProvider: NewBaseProvider(
			"test-provider",
			ProviderConfig{
				Name:     "Test Provider",
				Provider: "test",
				Config:   &BasicConfig{},
			},
			NewProviderCapabilities(),
		),
	}
}

func (p *workflowRequestProviderStub) Initialize(identifier string, provider ProviderConfig) error {
	return nil
}
func (p *workflowRequestProviderStub) RegisterWorkflows() any  { return nil }
func (p *workflowRequestProviderStub) RegisterActivities() any { return nil }
func (p *workflowRequestProviderStub) Synchronize(ctx context.Context, temporalClient TemporalImpl, req *SynchronizeRequest) error {
	return nil
}
func (p *workflowRequestProviderStub) ValidateRole(ctx context.Context, identity *Identity, role *Role) (map[string]any, error) {
	return nil, nil
}

func TestCreateAuthorizeRoleRequest_UsesResolvedIdentitySnapshot(t *testing.T) {
	cfg := &workflowRequestConfigStub{
		identityErr: errors.New("should not use config lookup when resolved identity is present"),
	}
	provider := newWorkflowRequestProviderStub()
	resolvedIdentity := &Identity{
		ID: "user@example.com",
		User: &User{
			Email:    "user@example.com",
			Username: "exampleuser",
			Name:     "Example User",
		},
	}

	req := &WorkflowRoleRequest{
		WorkflowID:       "wf-1",
		Identity:         "user@example.com",
		ResolvedIdentity: resolvedIdentity,
		Role: &Role{
			Identifier: "local_sudo",
			Name:       "Local Sudo",
			Permissions: RolePermissions{
				Allow: RoleStatements{Statement{Operations: []string{"sudo:*"}}},
			},
		},
	}

	result, err := CreateAuthorizeRoleRequest(cfg, provider, req)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Identity)
	require.NotNil(t, result.Identity.User)
	assert.Equal(t, "exampleuser", result.Identity.User.Username)
	assert.Equal(t, resolvedIdentity, result.Identity)
}

func TestCreateAuthorizeRoleRequest_FallsBackToConfigLookup(t *testing.T) {
	resolvedFromConfig := &Identity{
		ID: "user@example.com",
		User: &User{
			Email:    "user@example.com",
			Username: "exampleuser",
		},
	}
	cfg := &workflowRequestConfigStub{identity: resolvedFromConfig}
	provider := newWorkflowRequestProviderStub()

	req := &WorkflowRoleRequest{
		WorkflowID: "wf-2",
		Identity:   "user@example.com",
		Role: &Role{
			Identifier: "local_sudo",
			Name:       "Local Sudo",
			Permissions: RolePermissions{
				Allow: RoleStatements{Statement{Operations: []string{"sudo:*"}}},
			},
		},
	}

	result, err := CreateAuthorizeRoleRequest(cfg, provider, req)
	require.NoError(t, err)
	assert.Equal(t, resolvedFromConfig, result.Identity)
	assert.Equal(t, "exampleuser", result.Identity.User.Username)
}

func TestCreateAuthorizeRoleRequest_UsesSyntheticFallbackWhenLookupFails(t *testing.T) {
	cfg := &workflowRequestConfigStub{identityErr: errors.New("identity not found")}
	provider := newWorkflowRequestProviderStub()

	req := &WorkflowRoleRequest{
		WorkflowID: "wf-3",
		Identity:   "user@example.com",
		Role: &Role{
			Identifier: "local_sudo",
			Name:       "Local Sudo",
			Permissions: RolePermissions{
				Allow: RoleStatements{Statement{Operations: []string{"sudo:*"}}},
			},
		},
	}

	result, err := CreateAuthorizeRoleRequest(cfg, provider, req)
	require.NoError(t, err)
	require.NotNil(t, result.Identity)
	require.NotNil(t, result.Identity.User)
	assert.Equal(t, "user@example.com", result.Identity.ID)
	assert.Equal(t, "user@example.com", result.Identity.User.Email)
	assert.Empty(t, result.Identity.User.Username)
}

var _ ConfigImpl = (*workflowRequestConfigStub)(nil)
var _ Provider = (*workflowRequestProviderStub)(nil)
var _ = model.Endpoint{}
