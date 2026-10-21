// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package role

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/mcp/tool"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
)

// assigneeTypes lists the assignee types accepted by the role assignment tools.
var assigneeTypes = []string{
	string(AssigneeTypeUser), string(AssigneeTypeGroup), string(AssigneeTypeApp), string(AssigneeTypeAgent),
}

// roleTools provides MCP tools for managing roles and role assignments.
type roleTools struct {
	roleService       RoleServiceInterface
	assignmentService RoleAssignmentServiceInterface
	handler           *roleHandler
}

// roleListMCPInput is the input for the list roles tool.
type roleListMCPInput struct {
	tool.PaginationInput
}

// roleListMCPResponse is the MCP list tool output. HTTP pagination links are excluded.
type roleListMCPResponse struct {
	TotalResults int                   `json:"totalResults"`
	StartIndex   int                   `json:"startIndex"`
	Count        int                   `json:"count"`
	Roles        []RoleSummaryResponse `json:"roles"`
}

// roleUpdateMCPInput is the input for the update role tool.
type roleUpdateMCPInput struct {
	ID          string                `json:"id"                    jsonschema:"The unique identifier of the role"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	OUID        string                `json:"ouId"`
	Permissions []ResourcePermissions `json:"permissions"`
}

// roleAssignmentsGetMCPInput is the input for the get role assignments tool.
type roleAssignmentsGetMCPInput struct {
	ID string `json:"id" jsonschema:"The unique identifier of the role"`
	tool.PaginationInput
	Type           string `json:"type,omitempty"           jsonschema:"Optional assignee type to filter by"`
	IncludeDisplay bool   `json:"includeDisplay,omitempty" jsonschema:"Include display names of the assignees"`
}

// roleAssignmentListMCPResponse is the MCP assignments list output. HTTP pagination links are excluded.
type roleAssignmentListMCPResponse struct {
	TotalResults int                  `json:"totalResults"`
	StartIndex   int                  `json:"startIndex"`
	Count        int                  `json:"count"`
	Assignments  []AssignmentResponse `json:"assignments"`
}

// roleAssignmentsAddMCPInput is the input for the add role assignments tool.
type roleAssignmentsAddMCPInput struct {
	ID          string              `json:"id"          jsonschema:"The unique identifier of the role"`
	Assignments []AssignmentRequest `json:"assignments"`
}

// roleOperationMCPResponse is the MCP output for operations that return no resource.
type roleOperationMCPResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// registerMCPTools registers all role MCP tools with the server.
func registerMCPTools(
	server *mcp.Server, roleService RoleServiceInterface, assignmentService RoleAssignmentServiceInterface,
) {
	tools := &roleTools{
		roleService:       roleService,
		assignmentService: assignmentService,
		handler:           newRoleHandler(roleService, assignmentService),
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_list_roles",
		Description: `List roles with optional pagination.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Roles",
			ReadOnlyHint: true,
		},
	}, tools.listRoles)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_get_role",
		Description: `Retrieve full details of a role by ID including its permissions.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Role",
			ReadOnlyHint: true,
		},
	}, tools.getRole)

	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_create_role",
		Description: `Create a new role in an organization unit.

Prerequisites:
- Use thunderid_list_organization_units to pick the ouId.

Behavior:
- permissions groups permission strings by resource server ID.
- assignments optionally assigns the role to users, groups, apps, or agents on creation.`,
		InputSchema: getCreateRoleSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Create Role",
			IdempotentHint: false,
		},
	}, tools.createRole)

	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_update_role",
		Description: `Update an existing role (full replacement).

Workflow:
1. Use thunderid_get_role to get the current state
2. Modify the fields you want to change
3. Send the complete object back

Any field not provided will be reset to empty/default. Assignments are not changed by this tool.`,
		Annotations: &mcp.ToolAnnotations{
			Title:          "Update Role",
			IdempotentHint: true,
		},
	}, tools.updateRole)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_delete_role",
		Description: `Delete a role by ID. Declarative (read-only) roles cannot be deleted.`,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete Role",
			DestructiveHint: &[]bool{true}[0],
			IdempotentHint:  true,
		},
	}, tools.deleteRole)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_get_role_assignments",
		Description: `Retrieve the users, groups, apps, and agents assigned to a role, with optional pagination.`,
		InputSchema: tool.GenerateSchema[roleAssignmentsGetMCPInput](
			tool.WithEnum("", "type", assigneeTypes),
		),
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Role Assignments",
			ReadOnlyHint: true,
		},
	}, tools.getRoleAssignments)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_add_role_assignments",
		Description: `Assign a role to users, groups, apps, or agents.`,
		InputSchema: tool.GenerateSchema[roleAssignmentsAddMCPInput](
			tool.WithEnum("assignments", "type", assigneeTypes),
		),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Add Role Assignments",
			IdempotentHint: true,
		},
	}, tools.addRoleAssignments)
}

// listRoles handles the list_roles tool call.
func (t *roleTools) listRoles(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input roleListMCPInput,
) (*mcp.CallToolResult, *roleListMCPResponse, error) {
	limit := input.Limit
	if limit == 0 {
		limit = serverconst.DefaultPageSize
	}

	roleList, svcErr := t.roleService.GetRoleList(ctx, limit, input.Offset)
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to list roles: %s", svcErr.ErrorDescription)
	}

	roles := make([]RoleSummaryResponse, 0, len(roleList.Roles))
	for _, role := range roleList.Roles {
		roles = append(roles, RoleSummaryResponse(role))
	}

	return nil, &roleListMCPResponse{
		TotalResults: roleList.TotalResults,
		StartIndex:   roleList.StartIndex,
		Count:        roleList.Count,
		Roles:        roles,
	}, nil
}

// getRole handles the get_role tool call.
func (t *roleTools) getRole(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input tool.IDInput,
) (*mcp.CallToolResult, *RoleResponse, error) {
	role, svcErr := t.roleService.GetRoleWithPermissions(ctx, input.ID)
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to get role: %s", svcErr.ErrorDescription)
	}

	return nil, t.handler.toHTTPRoleResponse(role), nil
}

// createRole handles the create_role tool call.
func (t *roleTools) createRole(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input CreateRoleRequest,
) (*mcp.CallToolResult, *CreateRoleResponse, error) {
	sanitized := t.handler.sanitizeCreateRoleRequest(&input)
	role, svcErr := t.roleService.CreateRole(ctx, t.handler.toRoleCreationDetail(sanitized))
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to create role: %s", svcErr.ErrorDescription)
	}

	return nil, t.handler.toHTTPCreateRoleResponse(role), nil
}

// updateRole handles the update_role tool call with complete replacement.
func (t *roleTools) updateRole(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input roleUpdateMCPInput,
) (*mcp.CallToolResult, *RoleResponse, error) {
	sanitized := t.handler.sanitizeUpdateRoleRequest(&UpdateRoleRequest{
		Name:        input.Name,
		Description: input.Description,
		OUID:        input.OUID,
		Permissions: input.Permissions,
	})
	role, svcErr := t.roleService.UpdateRoleWithPermissions(ctx, input.ID, RoleUpdateDetail(sanitized))
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to update role: %s", svcErr.ErrorDescription)
	}

	return nil, t.handler.toHTTPRoleResponse(role), nil
}

// deleteRole handles the delete_role tool call.
func (t *roleTools) deleteRole(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input tool.IDInput,
) (*mcp.CallToolResult, *roleOperationMCPResponse, error) {
	if svcErr := t.roleService.DeleteRole(ctx, input.ID); svcErr != nil {
		return nil, nil, fmt.Errorf("failed to delete role: %s", svcErr.ErrorDescription)
	}

	return nil, &roleOperationMCPResponse{ID: input.ID, Message: "Role deleted successfully"}, nil
}

// getRoleAssignments handles the get_role_assignments tool call.
func (t *roleTools) getRoleAssignments(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input roleAssignmentsGetMCPInput,
) (*mcp.CallToolResult, *roleAssignmentListMCPResponse, error) {
	limit := input.Limit
	if limit == 0 {
		limit = serverconst.DefaultPageSize
	}

	var assignmentList *AssignmentList
	var svcErr *tidcommon.ServiceError
	if input.Type != "" {
		assignmentList, svcErr = t.assignmentService.GetRoleAssignmentsByType(
			ctx, input.ID, limit, input.Offset, input.IncludeDisplay, input.Type)
	} else {
		assignmentList, svcErr = t.assignmentService.GetRoleAssignments(
			ctx, input.ID, limit, input.Offset, input.IncludeDisplay)
	}
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to get role assignments: %s", svcErr.ErrorDescription)
	}

	assignments := make([]AssignmentResponse, len(assignmentList.Assignments))
	for i, a := range assignmentList.Assignments {
		assignments[i] = AssignmentResponse(a)
	}

	return nil, &roleAssignmentListMCPResponse{
		TotalResults: assignmentList.TotalResults,
		StartIndex:   assignmentList.StartIndex,
		Count:        assignmentList.Count,
		Assignments:  assignments,
	}, nil
}

// addRoleAssignments handles the add_role_assignments tool call.
func (t *roleTools) addRoleAssignments(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input roleAssignmentsAddMCPInput,
) (*mcp.CallToolResult, *roleOperationMCPResponse, error) {
	sanitized := t.handler.sanitizeAssignmentsRequest(&AssignmentsRequest{Assignments: input.Assignments})
	if svcErr := t.assignmentService.AddAssignments(
		ctx, input.ID, t.handler.toRoleAssignments(sanitized)); svcErr != nil {
		return nil, nil, fmt.Errorf("failed to add role assignments: %s", svcErr.ErrorDescription)
	}

	return nil, &roleOperationMCPResponse{ID: input.ID, Message: "Role assignments added successfully"}, nil
}

// getCreateRoleSchema generates the schema for the create_role tool.
func getCreateRoleSchema() *jsonschema.Schema {
	return tool.GenerateSchema[CreateRoleRequest](
		tool.WithEnum("assignments", "type", assigneeTypes),
	)
}
