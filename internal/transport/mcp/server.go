package mcptransport

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
	"sort"
	"xmock.local/x-mock-mcp/internal/app"
	"xmock.local/x-mock-mcp/internal/generation"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/internal/validation"
	"xmock.local/x-mock-mcp/pluginapi"
)

type refArgs struct {
	Role     pluginapi.Role `json:"role"`
	PluginID string         `json:"plugin_id"`
	Version  string         `json:"version"`
}

func (r refArgs) ref() pluginapi.Reference {
	return pluginapi.Reference{Role: r.Role, ID: r.PluginID, Version: r.Version}
}

type capabilityArgs struct {
	Role       pluginapi.Role `json:"role,omitempty"`
	PluginID   string         `json:"plugin_id,omitempty"`
	Version    string         `json:"version,omitempty"`
	SchemaName string         `json:"schema_name,omitempty"`
}
type prepareArgs struct {
	refArgs
	Input     json.RawMessage `json:"input"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
}
type putArgs struct {
	Document        scenario.Document `json:"document"`
	ExpectedVersion int64             `json:"expected_version"`
}
type installArgs struct {
	PackagePath string `json:"package_path"`
	SHA256      string `json:"sha256"`
}
type startArgs struct {
	EnvironmentID string `json:"environment_id"`
	JobName       string `json:"job_name"`
}
type runArgs struct {
	RunID string `json:"run_id"`
}
type traceArgs struct {
	RunID    string `json:"run_id"`
	AfterSeq int64  `json:"after_seq"`
	Limit    int    `json:"limit"`
}
type exportArgs struct {
	RunID      string   `json:"run_id"`
	RequestIDs []string `json:"request_ids"`
}
type destroyArgs struct {
	EnvironmentID string `json:"environment_id"`
}

func Result(value any, err error) (*sdk.CallToolResult, error) {
	bad := err != nil
	if bad {
		f := &pluginapi.Failure{Code: pluginapi.Code(err), Message: err.Error()}
		var actual *pluginapi.Failure
		if errors.As(err, &actual) {
			f = actual
		}
		value = map[string]any{"error": f}
	}
	raw, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}}, StructuredContent: json.RawMessage(raw), IsError: bad}, nil
}
func add[T any](s *sdk.Server, name, description string, readOnly, destructive bool, fn func(context.Context, T) (any, error)) {
	// Control-plane opaque payloads are JSON objects. In particular, Claude Code
	// rejects the boolean `true` shorthand emitted for an unconstrained RawMessage.
	// Plugin-owned schemas still validate the fields inside these objects.
	inferred, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {Type: "object"}}})
	if err != nil {
		panic(err)
	}
	schemaBytes, err := json.Marshal(inferred)
	if err != nil {
		panic(err)
	}
	schema := json.RawMessage(schemaBytes)
	openWorld := false
	s.AddTool(&sdk.Tool{Name: name, Description: description, InputSchema: schema, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: &openWorld}}, func(ctx context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		if err := validation.Check(schema, r.Params.Arguments); err != nil {
			return Result(nil, pluginapi.Invalid("%s", err.Error()))
		}
		var input T
		if err := pluginapi.Decode(r.Params.Arguments, &input); err != nil {
			return Result(nil, err)
		}
		value, err := fn(ctx, input)
		return Result(value, err)
	})
}
func New(service *app.Service) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "x-mock-mcp", Version: "0.1.0"}, &sdk.ServerOptions{Instructions: "左端对接应用，右端对接外部依赖。读取右端 QA 输入契约，分析用户 QA/源码/DDL 后 prepare/put。run_start 后主动 requests_next/resolve；只有 Coding Agent 提供生成数据，无独立 LLM API。不要更改 QA 或业务代码来通过测试。"})
	add(s, "mock_capabilities", "列出插件摘要和具名任务，或按 role/plugin_id/version 读取插件的 qa_input、candidate、scenario、config、guide。", true, false, func(ctx context.Context, r capabilityArgs) (any, error) {
		if r.PluginID == "" {
			records, err := service.Catalog.Records()
			if err != nil {
				return nil, err
			}
			items := []any{}
			for _, v := range records {
				items = append(items, map[string]any{"ref": v.Ref, "enabled": v.Enabled, "active_instances": v.ActiveInstances, "sha256": v.ArchiveSHA256, "contracts": v.Manifest.Descriptor.Contracts, "features": v.Manifest.Descriptor.Features})
			}
			jobs := service.Jobs()
			sort.Strings(jobs)
			return map[string]any{"plugins": items, "jobs": jobs, "data_strategies": []string{"StrictReplay", "AgentFill"}, "left": "application", "right": "external dependency"}, nil
		}
		ref := pluginapi.Reference{Role: r.Role, ID: r.PluginID, Version: r.Version}
		m, _, err := service.Catalog.Package(ref)
		if err != nil {
			return nil, err
		}
		d := m.Descriptor
		switch r.SchemaName {
		case "":
			return d, nil
		case "config":
			return map[string]any{"schema": d.ConfigSchema}, nil
		case "scenario":
			return map[string]any{"schema": d.ScenarioSchema}, nil
		case "qa_input", "candidate", "guide":
			if d.Preparation == nil {
				return nil, pluginapi.Fail("UNSUPPORTED", "plugin has no preparation contract")
			}
			if r.SchemaName == "guide" {
				return map[string]any{"guide": d.Preparation.Guide}, nil
			}
			schema := d.Preparation.InputSchema
			if r.SchemaName == "candidate" {
				schema = d.Preparation.CandidateSchema
			}
			return map[string]any{"schema": schema}, nil
		default:
			return nil, pluginapi.Invalid("unknown schema_name")
		}
	})
	add(s, "mock_plugin_install", "安装独立版本的左端或右端插件包，校验 SHA-256；安装后仍需启用。", false, false, func(ctx context.Context, r installArgs) (any, error) {
		return service.Catalog.Install(ctx, r.PackagePath, r.SHA256)
	})
	add(s, "mock_plugin_enable", "启用一个已安装插件版本。", false, false, func(ctx context.Context, r refArgs) (any, error) {
		return map[string]bool{"enabled": true}, service.Catalog.Enable(r.ref())
	})
	add(s, "mock_plugin_disable", "停用无活动实例的插件版本。", false, true, func(ctx context.Context, r refArgs) (any, error) {
		return map[string]bool{"enabled": false}, service.Catalog.Disable(r.ref())
	})
	add(s, "mock_plugin_uninstall", "卸载已停用且无活动引用的单个插件版本；另一端独立保留。", false, true, func(ctx context.Context, r refArgs) (any, error) {
		return map[string]bool{"uninstalled": true}, service.Catalog.Uninstall(r.ref())
	})
	add(s, "mock_scenario_prepare", "调用固定右端版本校验 QA/项目资料及候选；短暂启动准备进程，无应用监听，不保存场景。", false, false, func(ctx context.Context, r prepareArgs) (any, error) {
		return service.Prepare(ctx, r.ref(), r.Input, r.Candidate)
	})
	add(s, "mock_scenario_put", "重新准备校验后按 expected_version 原子保存新场景版本。", false, false, func(ctx context.Context, r putArgs) (any, error) {
		return service.Scenarios.Put(ctx, r.Document, r.ExpectedVersion)
	})
	add(s, "mock_environment_create", "固定场景/插件快照并启动左右端 binding；每个环境只运行一次任务。", false, false, func(ctx context.Context, r app.EnvironmentRequest) (any, error) {
		return service.CreateEnvironment(ctx, r)
	})
	add(s, "mock_run_start", "后台启动项目配置中的具名测试任务，立即返回 run_id；继续领取依赖缺口。", false, false, func(ctx context.Context, r startArgs) (any, error) {
		return service.StartRun(r.EnvironmentID, r.JobName)
	})
	add(s, "mock_requests_next", "领取缺口（最多8个，等待最多2秒）；首次获得 owner token，显式 takeover 需要当前 epoch。", false, false, func(ctx context.Context, r generation.NextRequest) (any, error) {
		status, err := service.Runs.Status(r.RunID)
		if err != nil {
			return nil, err
		}
		if status.State != "running" {
			return nil, pluginapi.Fail("STATE_CONFLICT", "run no longer accepts generation")
		}
		return service.Queue.Next(ctx, r)
	})
	add(s, "mock_requests_resolve", "回填符合插件 schema 的候选；等待右端实际完成校验与请求执行后确认。", false, false, func(ctx context.Context, r generation.ResolveRequest) (any, error) {
		return service.Queue.Resolve(ctx, r)
	})
	add(s, "mock_run_status", "查询后台任务状态。", true, false, func(ctx context.Context, r runArgs) (any, error) {
		status, err := service.Runs.Status(r.RunID)
		return map[string]any{"run": status, "pending_requests": service.Queue.Pending(r.RunID)}, err
	})
	add(s, "mock_run_trace", "按递增 seq 读取运行证据；不含认证报文或配置凭据。", true, false, func(ctx context.Context, r traceArgs) (any, error) {
		if _, err := service.Runs.Status(r.RunID); err != nil {
			return nil, err
		}
		events, err := service.Trace.Read(r.RunID, r.AfterSeq, r.Limit)
		return map[string]any{"events": events}, err
	})
	add(s, "mock_scenario_export", "从成功运行显式选取已确认生成，由右端导出初态候选并重新校验；不自动保存。", false, false, func(ctx context.Context, r exportArgs) (any, error) {
		return service.Export(ctx, r.RunID, r.RequestIDs)
	})
	add(s, "mock_run_stop", "取消待处理请求并停止该运行所属测试进程组。", false, true, func(ctx context.Context, r runArgs) (any, error) {
		service.Queue.CancelRun(r.RunID, pluginapi.Fail("CANCELLED", "run stopped"))
		return map[string]bool{"stopping": true}, service.Runs.Stop(r.RunID)
	})
	add(s, "mock_environment_destroy", "停止运行并释放环境的左右端实例和监听器。", false, true, func(ctx context.Context, r destroyArgs) (any, error) {
		return map[string]bool{"destroyed": true}, service.DestroyEnvironment(ctx, r.EnvironmentID)
	})
	return s
}
