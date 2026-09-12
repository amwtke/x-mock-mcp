package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func withSource(t *testing.T, spec pluginapi.PreparationSpec, source map[string]any) pluginapi.PreparationSpec {
	t.Helper()
	var candidate map[string]any
	if err := json.Unmarshal(spec.Candidate, &candidate); err != nil {
		t.Fatal(err)
	}
	statement := candidate["database_scenario"].(map[string]any)["statements"].([]any)[0].(map[string]any)
	statement["source"] = source
	spec.Candidate = mysqlv1.Encode(candidate)
	return spec
}

func TestExplicitJavaSourceStrategy(t *testing.T) {
	spec, _, _ := preparationFixture(t)
	spec = withSource(t, spec, map[string]any{"strategy": "java-literal"})
	report, err := (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatalf("explicit Java strategy: %+v %v", report, err)
	}
}

func TestSourceStrategyCannotDowngradeXMLToLiteral(t *testing.T) {
	spec, in, body := preparationFixture(t)
	body.DatabaseScenario.Statements[0].SourcePath = "Mapper.xml"
	in.Sources = append(in.Sources, sourceFile(t, spec.ProjectRoot, "Mapper.xml", "code", "<mapper><!-- "+body.DatabaseScenario.Statements[0].SQL+" --></mapper>"))
	body.Evidence = in.Sources
	spec.Input, spec.Candidate = mysqlv1.Encode(in), mysqlv1.Encode(body)
	for _, source := range []map[string]any{nil, {"strategy": "java-literal"}, {"strategy": "unknown"}} {
		probe := spec
		if source != nil {
			probe = withSource(t, spec, source)
		}
		report, err := (&plugin{}).Prepare(context.Background(), probe)
		if err != nil || report.Ready {
			t.Fatalf("XML bypass accepted: %+v %v", report, err)
		}
		if len(report.Diagnostics) == 0 || !strings.Contains(report.Diagnostics[0].Message, "SOURCE_EVIDENCE") {
			t.Fatalf("unhelpful source diagnostic: %+v", report)
		}
	}
}
