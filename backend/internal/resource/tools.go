// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	"github.com/thunder-id/thunderid/internal/system/mcp/tool"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// resourceServerTools provides MCP tools for managing resource servers.
type resourceServerTools struct {
	resourceService ResourceServiceInterface
}

// resourceServerListMCPInput is the list tool input.
type resourceServerListMCPInput struct {
	Limit  int `json:"limit,omitempty"  jsonschema:"Maximum number of results to return, 1 to 100 (default: 30)"`
	Offset int `json:"offset,omitempty" jsonschema:"Offset for pagination (default: 0)"`
}

// createResourceServerMCPInput is the create tool input.
//
//nolint:lll
type createResourceServerMCPInput struct {
	Name        string                       `json:"name" jsonschema:"Display name of the resource server"`
	Description string                       `json:"description,omitempty" jsonschema:"Optional description"`
	Identifier  string                       `json:"identifier" jsonschema:"Unique identifier of the resource server, typically its audience URI (for an MCP server, the server URL)"`
	Type        providers.ResourceServerType `json:"type,omitempty" jsonschema:"Resource server type. Defaults to CUSTOM when omitted."`
	OUID        string                       `json:"ouId" jsonschema:"ID of the organization unit that owns the resource server"`
	Delimiter   string                       `json:"delimiter,omitempty" jsonschema:"Single character joining resource and action handles into permission strings. Defaults to the server's configured delimiter when omitted."`
}

// updateResourceServerMCPInput is the update tool input.
//
//nolint:lll
type updateResourceServerMCPInput struct {
	ID                  string                              `json:"id" jsonschema:"ID of the resource server to update"`
	Name                string                              `json:"name" jsonschema:"Display name of the resource server"`
	Description         string                              `json:"description,omitempty" jsonschema:"Optional description"`
	Identifier          string                              `json:"identifier,omitempty" jsonschema:"Unique identifier of the resource server, typically its audience URI"`
	OUID                string                              `json:"ouId" jsonschema:"ID of the organization unit that owns the resource server"`
	AuthorizationEngine providers.AuthorizationEngineConfig `json:"authorizationEngine,omitempty" jsonschema:"Authorization engine that evaluates access to the resource server. Omit to use the default role-based engine."`
}

// resourceServerListMCPResponse is the list tool output. HTTP pagination links are excluded.
type resourceServerListMCPResponse struct {
	TotalResults    int                      `json:"totalResults"`
	StartIndex      int                      `json:"startIndex"`
	Count           int                      `json:"count"`
	ResourceServers []ResourceServerResponse `json:"resourceServers"`
}

// resourceServerDeleteMCPResponse is the delete tool output.
type resourceServerDeleteMCPResponse struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// registerMCPTools registers all resource server MCP tools with the server.
func registerMCPTools(server *mcp.Server, resourceService ResourceServiceInterface) {
	tools := &resourceServerTools{resourceService: resourceService}

	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_list_resource_servers",
		Description: `List resource servers with pagination. A resource server represents an API or MCP ` +
			`server protected by ThunderID and defines the resources and scopes applications request access to.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List Resource Servers",
			ReadOnlyHint: true,
		},
	}, tools.listResourceServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thunderid_get_resource_server",
		Description: `Retrieve details of a resource server by ID.`,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Resource Server",
			ReadOnlyHint: true,
		},
	}, tools.getResourceServer)

	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_create_resource_server",
		Description: `Register a new resource server.

Prerequisites:
- Use thunderid_list_organization_units to pick the organization unit that owns the resource server.

Behavior:
- Use type MCP to register an MCP server, API for a REST API. Defaults to CUSTOM.
- type and delimiter cannot be changed after creation.`,
		InputSchema: getCreateResourceServerSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Create Resource Server",
			IdempotentHint: false,
		},
	}, tools.createResourceServer)

	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_update_resource_server",
		Description: `Update an existing resource server (full replacement).

Workflow:
1. Use thunderid_get_resource_server to get the current state.
2. Modify the fields you want to change.
3. Send the complete object back.

Any field not provided will be reset to empty/default. type and delimiter cannot be updated.`,
		InputSchema: getUpdateResourceServerSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Update Resource Server",
			IdempotentHint: true,
		},
	}, tools.updateResourceServer)

	destructive := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "thunderid_delete_resource_server",
		Description: `Delete a resource server by ID. A resource server that still has resources or ` +
			`actions cannot be deleted; delete them first.`,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete Resource Server",
			DestructiveHint: &destructive,
			IdempotentHint:  true,
		},
	}, tools.deleteResourceServer)
}

// listResourceServers handles the list_resource_servers tool call.
func (t *resourceServerTools) listResourceServers(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input resourceServerListMCPInput,
) (*mcp.CallToolResult, *resourceServerListMCPResponse, error) {
	limit := input.Limit
	if limit == 0 {
		limit = serverconst.DefaultPageSize
	}

	result, svcErr := t.resourceService.GetResourceServerList(ctx, limit, input.Offset)
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to list resource servers: %s", svcErr.ErrorDescription)
	}

	response := toResourceServerListResponse(result)
	return nil, &resourceServerListMCPResponse{
		TotalResults:    response.TotalResults,
		StartIndex:      response.StartIndex,
		Count:           response.Count,
		ResourceServers: response.ResourceServers,
	}, nil
}

// getResourceServer handles the get_resource_server tool call.
func (t *resourceServerTools) getResourceServer(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input tool.IDInput,
) (*mcp.CallToolResult, *ResourceServerResponse, error) {
	result, svcErr := t.resourceService.GetResourceServer(ctx, input.ID)
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to get resource server: %s", svcErr.ErrorDescription)
	}

	return nil, toResourceServerResponse(result), nil
}

// createResourceServer handles the create_resource_server tool call.
func (t *resourceServerTools) createResourceServer(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input createResourceServerMCPInput,
) (*mcp.CallToolResult, *ResourceServerResponse, error) {
	sanitized := sanitizeCreateResourceServerRequest(&CreateResourceServerRequest{
		Name:        input.Name,
		Description: input.Description,
		Identifier:  input.Identifier,
		Type:        input.Type,
		OUID:        input.OUID,
		Delimiter:   input.Delimiter,
	})

	result, svcErr := t.resourceService.CreateResourceServer(ctx, providers.ResourceServer{
		Name:        sanitized.Name,
		Description: sanitized.Description,
		Identifier:  sanitized.Identifier,
		Type:        sanitized.Type,
		OUID:        sanitized.OUID,
		Delimiter:   sanitized.Delimiter,
	})
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to create resource server: %s", svcErr.ErrorDescription)
	}

	return nil, toResourceServerResponse(result), nil
}

// updateResourceServer handles the update_resource_server tool call with complete replacement.
func (t *resourceServerTools) updateResourceServer(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input updateResourceServerMCPInput,
) (*mcp.CallToolResult, *ResourceServerResponse, error) {
	sanitized := sanitizeUpdateResourceServerRequest(&UpdateResourceServerRequest{
		Name:                input.Name,
		Description:         input.Description,
		Identifier:          input.Identifier,
		OUID:                input.OUID,
		AuthorizationEngine: input.AuthorizationEngine,
	})

	result, svcErr := t.resourceService.UpdateResourceServer(ctx, input.ID, providers.ResourceServer{
		Name:                sanitized.Name,
		Description:         sanitized.Description,
		Identifier:          sanitized.Identifier,
		OUID:                sanitized.OUID,
		AuthorizationEngine: sanitized.AuthorizationEngine,
	})
	if svcErr != nil {
		return nil, nil, fmt.Errorf("failed to update resource server: %s", svcErr.ErrorDescription)
	}

	return nil, toResourceServerResponse(result), nil
}

// deleteResourceServer handles the delete_resource_server tool call.
func (t *resourceServerTools) deleteResourceServer(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input tool.IDInput,
) (*mcp.CallToolResult, *resourceServerDeleteMCPResponse, error) {
	if svcErr := t.resourceService.DeleteResourceServer(ctx, input.ID); svcErr != nil {
		return nil, nil, fmt.Errorf("failed to delete resource server: %s", svcErr.ErrorDescription)
	}

	return nil, &resourceServerDeleteMCPResponse{ID: input.ID, Deleted: true}, nil
}

// resourceServerTypeValues returns the supported resource server types as strings.
func resourceServerTypeValues() []string {
	return []string{
		string(providers.ResourceServerTypeAPI),
		string(providers.ResourceServerTypeMCP),
		string(providers.ResourceServerTypeCustom),
	}
}

// getCreateResourceServerSchema generates the schema for the create_resource_server tool.
func getCreateResourceServerSchema() *jsonschema.Schema {
	return tool.GenerateSchema[createResourceServerMCPInput](
		tool.WithEnum("", "type", resourceServerTypeValues()),
	)
}

// getUpdateResourceServerSchema generates the schema for the update_resource_server tool.
func getUpdateResourceServerSchema() *jsonschema.Schema {
	return tool.GenerateSchema[updateResourceServerMCPInput](
		tool.WithEnum("authorizationEngine", "type", []string{
			providers.AuthorizationEngineTypeRBAC,
			providers.AuthorizationEngineTypeAuthZENPDP,
		}),
	)
}
