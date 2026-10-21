// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	serverconst "github.com/thunder-id/thunderid/internal/system/constants"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

type ResourceServerToolsTestSuite struct {
	suite.Suite
	mockService *ResourceServiceInterfaceMock
	session     *mcp.ClientSession
}

func TestResourceServerToolsTestSuite(t *testing.T) {
	suite.Run(t, new(ResourceServerToolsTestSuite))
}

// SetupTest registers the tools over a mocked resource service and connects an in-memory MCP
// client, so every call crosses the SDK's schema validation and output encoding.
func (s *ResourceServerToolsTestSuite) SetupTest() {
	s.mockService = NewResourceServiceInterfaceMock(s.T())

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	registerMCPTools(server, s.mockService)
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

func (s *ResourceServerToolsTestSuite) call(name string, args map[string]any) *mcp.CallToolResult {
	result, err := s.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	s.Require().NoError(err)
	return result
}

func (s *ResourceServerToolsTestSuite) errorText(result *mcp.CallToolResult) string {
	s.Require().True(result.IsError)
	s.Require().NotEmpty(result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	s.Require().True(ok)
	return text.Text
}

func (s *ResourceServerToolsTestSuite) structured(result *mcp.CallToolResult, out any) {
	s.Require().False(result.IsError, "tool returned an error: %v", result.Content)
	raw, err := json.Marshal(result.StructuredContent)
	s.Require().NoError(err)
	s.Require().NoError(json.Unmarshal(raw, out))
}

func testToolResourceServer() *providers.ResourceServer {
	return &providers.ResourceServer{
		ID:         "rs-1",
		Name:       "Weather MCP",
		Identifier: "https://mcp.example.com",
		Type:       providers.ResourceServerTypeMCP,
		OUID:       "ou-1",
		Delimiter:  ":",
		AuthorizationEngine: providers.AuthorizationEngineConfig{
			Type: providers.AuthorizationEngineTypeRBAC,
		},
	}
}

func (s *ResourceServerToolsTestSuite) TestToolsAreRegistered() {
	result, err := s.session.ListTools(context.Background(), nil)
	s.Require().NoError(err)

	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, t := range result.Tools {
		tools[t.Name] = t
	}
	s.Len(tools, 5)
	s.True(tools["thunderid_list_resource_servers"].Annotations.ReadOnlyHint)
	s.True(tools["thunderid_get_resource_server"].Annotations.ReadOnlyHint)
	s.False(tools["thunderid_create_resource_server"].Annotations.ReadOnlyHint)
	s.False(tools["thunderid_update_resource_server"].Annotations.ReadOnlyHint)
	s.Require().NotNil(tools["thunderid_delete_resource_server"].Annotations.DestructiveHint)
	s.True(*tools["thunderid_delete_resource_server"].Annotations.DestructiveHint)
}

func (s *ResourceServerToolsTestSuite) TestListResourceServers_DefaultPagination() {
	s.mockService.On("GetResourceServerList", mock.Anything, serverconst.DefaultPageSize, 0).
		Return(&ResourceServerList{
			TotalResults:    1,
			StartIndex:      1,
			Count:           1,
			ResourceServers: []providers.ResourceServer{*testToolResourceServer()},
			Links:           []Link{{Href: "resource-servers?offset=30&limit=30", Rel: "next"}},
		}, (*tidcommon.ServiceError)(nil))

	var out resourceServerListMCPResponse
	s.structured(s.call("thunderid_list_resource_servers", map[string]any{}), &out)

	s.Equal(1, out.TotalResults)
	s.Equal(1, out.Count)
	s.Require().Len(out.ResourceServers, 1)
	s.Equal("rs-1", out.ResourceServers[0].ID)
	s.Equal(providers.ResourceServerTypeMCP, out.ResourceServers[0].Type)
}

func (s *ResourceServerToolsTestSuite) TestListResourceServers_WithPagination() {
	s.mockService.On("GetResourceServerList", mock.Anything, 5, 10).
		Return(&ResourceServerList{TotalResults: 10, StartIndex: 11}, (*tidcommon.ServiceError)(nil))

	var out resourceServerListMCPResponse
	s.structured(s.call("thunderid_list_resource_servers", map[string]any{"limit": 5, "offset": 10}), &out)

	s.Equal(10, out.TotalResults)
	s.Empty(out.ResourceServers)
}

func (s *ResourceServerToolsTestSuite) TestListResourceServers_ServiceError() {
	s.mockService.On("GetResourceServerList", mock.Anything, 500, 0).
		Return((*ResourceServerList)(nil), &ErrorInvalidLimit)

	text := s.errorText(s.call("thunderid_list_resource_servers", map[string]any{"limit": 500}))
	s.Contains(text, "failed to list resource servers")
	s.Contains(text, ErrorInvalidLimit.ErrorDescription.DefaultValue)
}

func (s *ResourceServerToolsTestSuite) TestGetResourceServer() {
	s.mockService.On("GetResourceServer", mock.Anything, "rs-1").
		Return(testToolResourceServer(), (*tidcommon.ServiceError)(nil))

	var out ResourceServerResponse
	s.structured(s.call("thunderid_get_resource_server", map[string]any{"id": "rs-1"}), &out)

	s.Equal("rs-1", out.ID)
	s.Equal("https://mcp.example.com", out.Identifier)
	s.Equal(providers.AuthorizationEngineTypeRBAC, out.AuthorizationEngine.Type)
}

func (s *ResourceServerToolsTestSuite) TestGetResourceServer_NotFound() {
	s.mockService.On("GetResourceServer", mock.Anything, "missing").
		Return((*providers.ResourceServer)(nil), &ErrorResourceServerNotFound)

	text := s.errorText(s.call("thunderid_get_resource_server", map[string]any{"id": "missing"}))
	s.Contains(text, "failed to get resource server")
}

func (s *ResourceServerToolsTestSuite) TestCreateResourceServer() {
	s.mockService.On("CreateResourceServer", mock.Anything, providers.ResourceServer{
		Name:       "Weather MCP",
		Identifier: "https://mcp.example.com",
		Type:       providers.ResourceServerTypeMCP,
		OUID:       "ou-1",
		Delimiter:  ":",
	}).Return(testToolResourceServer(), (*tidcommon.ServiceError)(nil))

	var out ResourceServerResponse
	s.structured(s.call("thunderid_create_resource_server", map[string]any{
		"name":       "Weather MCP",
		"identifier": "https://mcp.example.com",
		"type":       "MCP",
		"ouId":       "ou-1",
		"delimiter":  ":",
	}), &out)

	s.Equal("rs-1", out.ID)
}

func (s *ResourceServerToolsTestSuite) TestCreateResourceServer_InvalidTypeRejectedBySchema() {
	result := s.call("thunderid_create_resource_server", map[string]any{
		"name": "Weather MCP", "identifier": "https://mcp.example.com", "type": "GRAPHQL", "ouId": "ou-1",
	})

	s.True(result.IsError)
	s.mockService.AssertNotCalled(s.T(), "CreateResourceServer", mock.Anything, mock.Anything)
}

func (s *ResourceServerToolsTestSuite) TestCreateResourceServer_MissingRequiredField() {
	result := s.call("thunderid_create_resource_server", map[string]any{"name": "Weather MCP"})

	s.True(result.IsError)
	s.mockService.AssertNotCalled(s.T(), "CreateResourceServer", mock.Anything, mock.Anything)
}

func (s *ResourceServerToolsTestSuite) TestCreateResourceServer_ServiceError() {
	s.mockService.On("CreateResourceServer", mock.Anything, mock.Anything).
		Return((*providers.ResourceServer)(nil), &ErrorIdentifierConflict)

	text := s.errorText(s.call("thunderid_create_resource_server", map[string]any{
		"name": "Weather MCP", "identifier": "https://mcp.example.com", "ouId": "ou-1",
	}))
	s.Contains(text, "failed to create resource server")
}

func (s *ResourceServerToolsTestSuite) TestUpdateResourceServer() {
	engine := providers.AuthorizationEngineConfig{
		Type:       providers.AuthorizationEngineTypeAuthZENPDP,
		Properties: providers.AuthorizationEngineProperties{PDPConnectionID: "pdp-1"},
	}
	updated := testToolResourceServer()
	updated.Name = "Weather MCP v2"
	updated.AuthorizationEngine = engine
	s.mockService.On("UpdateResourceServer", mock.Anything, "rs-1", providers.ResourceServer{
		Name:                "Weather MCP v2",
		Identifier:          "https://mcp.example.com",
		OUID:                "ou-1",
		AuthorizationEngine: engine,
	}).Return(updated, (*tidcommon.ServiceError)(nil))

	var out ResourceServerResponse
	s.structured(s.call("thunderid_update_resource_server", map[string]any{
		"id":         "rs-1",
		"name":       "Weather MCP v2",
		"identifier": "https://mcp.example.com",
		"ouId":       "ou-1",
		"authorizationEngine": map[string]any{
			"type":       "authzen_pdp",
			"properties": map[string]any{"pdpConnectionId": "pdp-1"},
		},
	}), &out)

	s.Equal("Weather MCP v2", out.Name)
	s.Equal(providers.AuthorizationEngineTypeAuthZENPDP, out.AuthorizationEngine.Type)
}

func (s *ResourceServerToolsTestSuite) TestUpdateResourceServer_MissingID() {
	result := s.call("thunderid_update_resource_server", map[string]any{"name": "Weather MCP", "ouId": "ou-1"})

	s.True(result.IsError)
	s.mockService.AssertNotCalled(s.T(), "UpdateResourceServer", mock.Anything, mock.Anything, mock.Anything)
}

func (s *ResourceServerToolsTestSuite) TestUpdateResourceServer_ServiceError() {
	s.mockService.On("UpdateResourceServer", mock.Anything, "rs-1", mock.Anything).
		Return((*providers.ResourceServer)(nil), &ErrorResourceServerNotFound)

	text := s.errorText(s.call("thunderid_update_resource_server", map[string]any{
		"id": "rs-1", "name": "Weather MCP", "ouId": "ou-1",
	}))
	s.Contains(text, "failed to update resource server")
}

func (s *ResourceServerToolsTestSuite) TestDeleteResourceServer() {
	s.mockService.On("DeleteResourceServer", mock.Anything, "rs-1").Return((*tidcommon.ServiceError)(nil))

	var out resourceServerDeleteMCPResponse
	s.structured(s.call("thunderid_delete_resource_server", map[string]any{"id": "rs-1"}), &out)

	s.Equal("rs-1", out.ID)
	s.True(out.Deleted)
}

func (s *ResourceServerToolsTestSuite) TestDeleteResourceServer_ServiceError() {
	s.mockService.On("DeleteResourceServer", mock.Anything, "rs-1").Return(&ErrorImmutableResourceServer)

	text := s.errorText(s.call("thunderid_delete_resource_server", map[string]any{"id": "rs-1"}))
	s.Contains(text, "failed to delete resource server")
}
