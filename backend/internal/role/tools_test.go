// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package role

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

type RoleToolsTestSuite struct {
	suite.Suite
	mockRoleService       *RoleServiceInterfaceMock
	mockAssignmentService *RoleAssignmentServiceInterfaceMock
	session               *mcp.ClientSession
}

func TestRoleToolsTestSuite(t *testing.T) {
	suite.Run(t, new(RoleToolsTestSuite))
}

// SetupTest registers the role tools over mocked services and connects an in-memory MCP client, so
// every call crosses the SDK's schema validation and output encoding as an agent's would.
func (s *RoleToolsTestSuite) SetupTest() {
	s.mockRoleService = NewRoleServiceInterfaceMock(s.T())
	s.mockAssignmentService = NewRoleAssignmentServiceInterfaceMock(s.T())

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	registerMCPTools(server, s.mockRoleService, s.mockAssignmentService)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	s.session, err = client.Connect(ctx, clientTransport, nil)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = s.session.Close() })
}

func (s *RoleToolsTestSuite) call(name string, args map[string]any) *mcp.CallToolResult {
	result, err := s.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	s.Require().NoError(err)
	return result
}

func (s *RoleToolsTestSuite) errorText(result *mcp.CallToolResult) string {
	s.Require().True(result.IsError)
	s.Require().NotEmpty(result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	s.Require().True(ok)
	return text.Text
}

func (s *RoleToolsTestSuite) structured(result *mcp.CallToolResult, out any) {
	s.Require().False(result.IsError, "tool returned an error: %v", result.Content)
	raw, err := json.Marshal(result.StructuredContent)
	s.Require().NoError(err)
	s.Require().NoError(json.Unmarshal(raw, out))
}

func (s *RoleToolsTestSuite) decodeSchema(schema any, out any) {
	raw, err := json.Marshal(schema)
	s.Require().NoError(err)
	s.Require().NoError(json.Unmarshal(raw, out))
}

func (s *RoleToolsTestSuite) TestRegisterMCPTools() {
	result, err := s.session.ListTools(context.Background(), nil)
	s.Require().NoError(err)

	tools := map[string]*mcp.Tool{}
	for _, t := range result.Tools {
		tools[t.Name] = t
	}
	s.Len(tools, 7)
	for _, name := range []string{
		"thunderid_list_roles", "thunderid_get_role", "thunderid_get_role_assignments",
	} {
		s.Require().Contains(tools, name)
		s.True(tools[name].Annotations.ReadOnlyHint, name)
	}
	for _, name := range []string{
		"thunderid_create_role", "thunderid_update_role", "thunderid_delete_role",
		"thunderid_add_role_assignments",
	} {
		s.Require().Contains(tools, name)
		s.False(tools[name].Annotations.ReadOnlyHint, name)
	}
	s.Require().NotNil(tools["thunderid_delete_role"].Annotations.DestructiveHint)
	s.True(*tools["thunderid_delete_role"].Annotations.DestructiveHint)

	var createSchema, updateSchema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	s.decodeSchema(tools["thunderid_create_role"].InputSchema, &createSchema)
	s.decodeSchema(tools["thunderid_update_role"].InputSchema, &updateSchema)
	s.NotContains(createSchema.Properties, "id")
	s.Contains(createSchema.Required, "name")
	s.Contains(createSchema.Required, "ouId")
	s.Contains(updateSchema.Required, "id")
	s.NotContains(updateSchema.Properties, "assignments")
}

func (s *RoleToolsTestSuite) TestListRoles_DefaultPagination() {
	s.mockRoleService.On("GetRoleList", mock.Anything, serverconst.DefaultPageSize, 0).Return(&RoleList{
		TotalResults: 1, StartIndex: 1, Count: 1,
		Roles: []Role{{ID: "role-1", Name: "Admin", OUID: "ou-1", IsReadOnly: true}},
	}, (*tidcommon.ServiceError)(nil))

	var out roleListMCPResponse
	s.structured(s.call("thunderid_list_roles", map[string]any{}), &out)

	s.Equal(1, out.TotalResults)
	s.Require().Len(out.Roles, 1)
	s.Equal("role-1", out.Roles[0].ID)
	s.True(out.Roles[0].IsReadOnly)
}

func (s *RoleToolsTestSuite) TestListRoles_CustomPagination() {
	s.mockRoleService.On("GetRoleList", mock.Anything, 5, 10).
		Return(&RoleList{Roles: []Role{}}, (*tidcommon.ServiceError)(nil))

	var out roleListMCPResponse
	s.structured(s.call("thunderid_list_roles", map[string]any{"limit": 5, "offset": 10}), &out)

	s.Empty(out.Roles)
}

func (s *RoleToolsTestSuite) TestListRoles_ServiceError() {
	s.mockRoleService.On("GetRoleList", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &ErrorInvalidLimit)

	text := s.errorText(s.call("thunderid_list_roles", map[string]any{"limit": -1}))

	s.Contains(text, "failed to list roles")
	s.Contains(text, ErrorInvalidLimit.ErrorDescription.DefaultValue)
}

func (s *RoleToolsTestSuite) TestGetRole() {
	s.mockRoleService.On("GetRoleWithPermissions", mock.Anything, "role-1").Return(&RoleWithPermissions{
		ID: "role-1", Name: "Admin", OUID: "ou-1",
		Permissions: []ResourcePermissions{{ResourceServerID: "rs-1", Permissions: []string{"read"}}},
	}, (*tidcommon.ServiceError)(nil))

	var out RoleResponse
	s.structured(s.call("thunderid_get_role", map[string]any{"id": "role-1"}), &out)

	s.Equal("Admin", out.Name)
	s.Require().Len(out.Permissions, 1)
	s.Equal([]string{"read"}, out.Permissions[0].Permissions)
}

func (s *RoleToolsTestSuite) TestGetRole_NotFound() {
	s.mockRoleService.On("GetRoleWithPermissions", mock.Anything, "missing").
		Return(nil, &ErrorRoleNotFound)

	text := s.errorText(s.call("thunderid_get_role", map[string]any{"id": "missing"}))

	s.Contains(text, "failed to get role")
	s.Contains(text, ErrorRoleNotFound.ErrorDescription.DefaultValue)
}

func (s *RoleToolsTestSuite) TestCreateRole() {
	var received RoleCreationDetail
	s.mockRoleService.On("CreateRole", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { received = args.Get(1).(RoleCreationDetail) }).
		Return(&RoleWithPermissionsAndAssignments{
			ID: "role-1", Name: "Editor", OUID: "ou-1",
			Assignments: []RoleAssignment{{ID: "user-1", Type: AssigneeTypeUser}},
		}, (*tidcommon.ServiceError)(nil))

	var out CreateRoleResponse
	s.structured(s.call("thunderid_create_role", map[string]any{
		"name": "  Editor  ", "ouId": "ou-1",
		"permissions": []any{map[string]any{"resourceServerId": "rs-1", "permissions": []any{"write"}}},
		"assignments": []any{map[string]any{"id": "user-1", "type": "user"}},
	}), &out)

	s.Equal("Editor", received.Name)
	s.Equal("ou-1", received.OUID)
	s.Equal([]RoleAssignment{{ID: "user-1", Type: AssigneeTypeUser}}, received.Assignments)
	s.Equal("role-1", out.ID)
	s.NotNil(out.Permissions)
	s.Require().Len(out.Assignments, 1)
}

func (s *RoleToolsTestSuite) TestCreateRole_RejectsInvalidAssigneeType() {
	result := s.call("thunderid_create_role", map[string]any{
		"name": "Editor", "ouId": "ou-1", "permissions": []any{},
		"assignments": []any{map[string]any{"id": "x", "type": "robot"}},
	})

	s.True(result.IsError)
	s.mockRoleService.AssertNotCalled(s.T(), "CreateRole", mock.Anything, mock.Anything)
}

func (s *RoleToolsTestSuite) TestCreateRole_ServiceError() {
	s.mockRoleService.On("CreateRole", mock.Anything, mock.Anything).Return(nil, &ErrorRoleNameConflict)

	text := s.errorText(s.call("thunderid_create_role", map[string]any{
		"name": "Editor", "ouId": "ou-1", "permissions": []any{},
	}))

	s.Contains(text, "failed to create role")
}

func (s *RoleToolsTestSuite) TestUpdateRole() {
	expected := RoleUpdateDetail{
		Name: "Editor", Description: "Edits", OUID: "ou-1",
		Permissions: []ResourcePermissions{{ResourceServerID: "rs-1", Permissions: []string{"write"}}},
	}
	s.mockRoleService.On("UpdateRoleWithPermissions", mock.Anything, "role-1", expected).
		Return(&RoleWithPermissions{ID: "role-1", Name: "Editor", OUID: "ou-1",
			Permissions: expected.Permissions}, (*tidcommon.ServiceError)(nil))

	var out RoleResponse
	s.structured(s.call("thunderid_update_role", map[string]any{
		"id": "role-1", "name": "Editor", "description": "Edits", "ouId": "ou-1",
		"permissions": []any{map[string]any{"resourceServerId": "rs-1", "permissions": []any{"write"}}},
	}), &out)

	s.Equal("role-1", out.ID)
	s.Equal(expected.Permissions, out.Permissions)
}

func (s *RoleToolsTestSuite) TestUpdateRole_ServiceError() {
	s.mockRoleService.On("UpdateRoleWithPermissions", mock.Anything, "role-1", mock.Anything).
		Return(nil, &ErrorRoleNotFound)

	text := s.errorText(s.call("thunderid_update_role", map[string]any{
		"id": "role-1", "name": "Editor", "ouId": "ou-1", "permissions": []any{},
	}))

	s.Contains(text, "failed to update role")
}

func (s *RoleToolsTestSuite) TestDeleteRole() {
	s.mockRoleService.On("DeleteRole", mock.Anything, "role-1").Return((*tidcommon.ServiceError)(nil))

	var out roleOperationMCPResponse
	s.structured(s.call("thunderid_delete_role", map[string]any{"id": "role-1"}), &out)

	s.Equal("role-1", out.ID)
}

func (s *RoleToolsTestSuite) TestDeleteRole_ServiceError() {
	s.mockRoleService.On("DeleteRole", mock.Anything, "role-1").Return(&ErrorRoleNotFound)

	text := s.errorText(s.call("thunderid_delete_role", map[string]any{"id": "role-1"}))

	s.Contains(text, "failed to delete role")
}

func (s *RoleToolsTestSuite) TestGetRoleAssignments() {
	s.mockAssignmentService.On("GetRoleAssignments", mock.Anything, "role-1",
		serverconst.DefaultPageSize, 0, true).Return(&AssignmentList{
		TotalResults: 1, StartIndex: 1, Count: 1,
		Assignments: []RoleAssignmentWithDisplay{{ID: "user-1", Type: AssigneeTypeUser, Display: "Alice"}},
	}, (*tidcommon.ServiceError)(nil))

	var out roleAssignmentListMCPResponse
	s.structured(s.call("thunderid_get_role_assignments", map[string]any{
		"id": "role-1", "includeDisplay": true,
	}), &out)

	s.Equal(1, out.TotalResults)
	s.Equal([]AssignmentResponse{{ID: "user-1", Type: AssigneeTypeUser, Display: "Alice"}}, out.Assignments)
}

func (s *RoleToolsTestSuite) TestGetRoleAssignments_ByType() {
	s.mockAssignmentService.On("GetRoleAssignmentsByType", mock.Anything, "role-1", 10, 5, false, "group").
		Return(&AssignmentList{Assignments: []RoleAssignmentWithDisplay{}}, (*tidcommon.ServiceError)(nil))

	var out roleAssignmentListMCPResponse
	s.structured(s.call("thunderid_get_role_assignments", map[string]any{
		"id": "role-1", "type": "group", "limit": 10, "offset": 5,
	}), &out)

	s.Empty(out.Assignments)
}

func (s *RoleToolsTestSuite) TestGetRoleAssignments_RejectsInvalidType() {
	result := s.call("thunderid_get_role_assignments", map[string]any{"id": "role-1", "type": "robot"})

	s.True(result.IsError)
	s.mockAssignmentService.AssertNotCalled(s.T(), "GetRoleAssignmentsByType",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func (s *RoleToolsTestSuite) TestGetRoleAssignments_ServiceError() {
	s.mockAssignmentService.On("GetRoleAssignments", mock.Anything, "role-1", mock.Anything, mock.Anything,
		false).Return(nil, &ErrorRoleNotFound)

	text := s.errorText(s.call("thunderid_get_role_assignments", map[string]any{"id": "role-1"}))

	s.Contains(text, "failed to get role assignments")
}

func (s *RoleToolsTestSuite) TestAddRoleAssignments() {
	s.mockAssignmentService.On("AddAssignments", mock.Anything, "role-1", []RoleAssignment{
		{ID: "user-1", Type: AssigneeTypeUser}, {ID: "group-1", Type: AssigneeTypeGroup},
	}).Return((*tidcommon.ServiceError)(nil))

	var out roleOperationMCPResponse
	s.structured(s.call("thunderid_add_role_assignments", map[string]any{
		"id": "role-1",
		"assignments": []any{
			map[string]any{"id": " user-1 ", "type": "user"},
			map[string]any{"id": "group-1", "type": "group"},
		},
	}), &out)

	s.Equal("role-1", out.ID)
}

func (s *RoleToolsTestSuite) TestAddRoleAssignments_ServiceError() {
	s.mockAssignmentService.On("AddAssignments", mock.Anything, "role-1", mock.Anything).
		Return(&ErrorInvalidAssignmentID)

	text := s.errorText(s.call("thunderid_add_role_assignments", map[string]any{
		"id": "role-1", "assignments": []any{map[string]any{"id": "nope", "type": "user"}},
	}))

	s.Contains(text, "failed to add role assignments")
}
