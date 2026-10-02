package jsonrpc

import (
	"context"

	"github.com/komari-monitor/komari/internal/trafficreport"
	"github.com/komari-monitor/komari/pkg/rpc"
)

func init() {
	RegisterWithGroupAndMeta("getTrafficReportConfiguration", rpc.RoleAdmin, adminGetTrafficReportConfiguration, &rpc.MethodMeta{
		Name: "admin:getTrafficReportConfiguration", Summary: "Get built-in scheduled traffic report configuration", Returns: "TrafficReportConfiguration",
	})
	RegisterWithGroupAndMeta("setTrafficReportConfiguration", rpc.RoleAdmin, adminSetTrafficReportConfiguration, &rpc.MethodMeta{
		Name: "admin:setTrafficReportConfiguration", Summary: "Save built-in scheduled traffic report configuration", Returns: "null",
	})
}

func adminGetTrafficReportConfiguration(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	cfg, err := trafficreport.GetConfiguration()
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return cfg, nil
}

func adminSetTrafficReportConfiguration(_ context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var cfg trafficreport.Configuration
	if err := req.BindParams(&cfg); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, "Invalid configuration: "+err.Error(), nil)
	}
	if err := trafficreport.Validate(&cfg); err != nil {
		return nil, rpc.MakeError(rpc.InvalidParams, err.Error(), nil)
	}
	if err := trafficreport.SaveConfiguration(cfg); err != nil {
		return nil, rpc.MakeError(rpc.InternalError, err.Error(), nil)
	}
	return nil, nil
}
