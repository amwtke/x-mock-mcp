package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func (p *plugin) Prepare(ctx context.Context, spec pluginapi.PreparationSpec) (pluginapi.PreparationReport, error) {
	report := pluginapi.PreparationReport{MissingInputs: []pluginapi.PreparationIssue{}, Ambiguities: []pluginapi.PreparationIssue{}, Unsupported: []pluginapi.PreparationIssue{}, Diagnostics: []pluginapi.PreparationIssue{}}
	missing := func(path, message string) {
		report.MissingInputs = append(report.MissingInputs, pluginapi.PreparationIssue{Code: "MISSING_INPUT", Path: path, Message: message})
	}
	problem := func(path, message string) {
		report.Diagnostics = append(report.Diagnostics, pluginapi.PreparationIssue{Code: "INVALID_CANDIDATE", Path: path, Message: message})
	}
	var input Input
	if err := pluginapi.Decode(spec.Input, &input); err != nil {
		problem("input", err.Error())
		return report, nil
	}
	for path, value := range map[string]string{"qa.id": input.QA.ID, "qa.goal": input.QA.Goal, "qa.natural_language": input.QA.NaturalLanguage, "qa.start_page": input.QA.StartPage, "qa.role": input.QA.Role, "qa.initial_state": input.QA.InitialState, "qa.data_policy": input.QA.DataPolicy, "qa.write_rules": input.QA.WriteRules, "qa.coverage": input.QA.Coverage} {
		if strings.TrimSpace(value) == "" {
			missing(path, "请提供对应业务资料；无相关规则时明确说明不涉及")
		}
	}
	steps := map[string]bool{}
	if len(input.QA.Steps) == 0 {
		missing("qa.steps", "至少提供一个自然语言操作和预期")
	}
	for i, step := range input.QA.Steps {
		if step.ID == "" || step.Action == "" || step.Expected == "" || steps[step.ID] {
			missing(fmt.Sprintf("qa.steps[%d]", i), "步骤需要唯一 ID、操作和可观察预期")
		}
		steps[step.ID] = true
	}
	for _, a := range input.QA.Ambiguities {
		report.Ambiguities = append(report.Ambiguities, pluginapi.PreparationIssue{Code: "AMBIGUOUS_REQUIREMENT", Path: "qa", Message: a})
	}
	files := map[string][]byte{}
	ddl := ""
	total := 0
	root, err := filepath.EvalSymlinks(spec.ProjectRoot)
	if err != nil {
		missing("project_root", "项目目录不存在")
		return report, nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return report, err
	}
	for _, source := range input.Sources {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !pluginapi.SafeRelative(source.Path) || !pluginapi.ValidDigest(source.SHA256) {
			problem("sources", "源文件路径或摘要不合法")
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(source.Path)))
		if err != nil {
			missing(source.Path, "源文件不存在")
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			problem(source.Path, "源文件必须在项目目录内")
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			missing(source.Path, err.Error())
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		file.Close()
		if err != nil {
			return report, err
		}
		total += len(raw)
		if len(raw) > 1<<20 || total > 2<<20 {
			problem(source.Path, "资料超出上限，请只引用当前用例相关文件")
			continue
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != source.SHA256 {
			problem(source.Path, "STALE_EVIDENCE: 文件内容已改变")
			continue
		}
		if _, exists := files[source.Path]; exists {
			problem(source.Path, "重复来源")
			continue
		}
		files[source.Path] = raw
		if source.Kind == "ddl" {
			ddl += string(raw) + "\n"
		}
	}
	if strings.TrimSpace(ddl) == "" {
		missing("sources.ddl", "必须提供可离线解析的最终 DDL")
	}
	if len(report.MissingInputs)+len(report.Diagnostics)+len(report.Ambiguities) > 0 {
		return report, nil
	}
	tables, err := ParseDDL(ddl)
	if err != nil {
		report.Unsupported = append(report.Unsupported, pluginapi.PreparationIssue{Code: "UNSUPPORTED_DDL", Path: "sources.ddl", Message: err.Error()})
		return report, nil
	}
	report.Instructions = "依据固定 QA、源码和 DDL 生成步骤/API/SQL 映射、元数据、参数域、初始实体和状态断言；不改变 QA 预期。"
	if len(spec.Candidate) == 0 {
		return report, nil
	}
	var body Bundle
	if err = pluginapi.Decode(spec.Candidate, &body); err != nil {
		problem("candidate", err.Error())
		return report, nil
	}
	if !reflect.DeepEqual(body.QAContract, input.QA) || !reflect.DeepEqual(body.Evidence, input.Sources) {
		problem("qa_contract/evidence", "候选必须保留输入 QA 和证据")
	}
	db := &body.DatabaseScenario
	db.Tables = tables
	if db.Database == "" || (db.Mode != "stateful" && db.Mode != "fixture") {
		problem("database_scenario", "需要数据库名及明确的 stateful/fixture 模式")
	}
	if db.Transaction == "" {
		db.Transaction = "READ-COMMITTED"
	}
	if db.Transaction != "READ-COMMITTED" {
		report.Unsupported = append(report.Unsupported, pluginapi.PreparationIssue{Code: "UNSUPPORTED_ISOLATION", Path: "transaction", Message: "P0 仅支持 READ COMMITTED 可观察行为"})
	}
	if err = validateInitial(tables, db.Initial); err != nil {
		problem("database_scenario.initial", err.Error())
	}
	statements := map[string]bool{}
	writtenTables := map[string]bool{}
	for i, statement := range db.Statements {
		if statement.ID == "" || statements[statement.ID] {
			problem("statements", "规则 ID 为空或重复")
			continue
		}
		statements[statement.ID] = true
		code, exists := files[statement.SourcePath]
		if !exists || !strings.Contains(strings.Join(strings.Fields(string(code)), " "), strings.Join(strings.Fields(statement.SQL), " ")) {
			problem(statement.SourcePath, "源码证据中未找到声明的 SQL；复杂动态 SQL 需要明确分支证据")
			continue
		}
		plan, err := Compile(statement, tables, db.Database)
		if err != nil {
			report.Unsupported = append(report.Unsupported, pluginapi.PreparationIssue{Code: "UNSUPPORTED_SQL", Path: statement.ID, Message: err.Error()})
			continue
		}
		for _, parameter := range statement.Parameters {
			if len(parameter.Allowed) == 0 {
				problem(statement.ID, "每个业务参数必须声明允许值域")
			}
		}
		if statement.Plan != nil && !reflect.DeepEqual(statement.Plan, plan) {
			problem(statement.ID, "候选执行计划与真实 SQL 不一致")
		}
		db.Statements[i].Plan = plan
		if plan.Kind != "select" {
			writtenTables[plan.Table] = true
		}
		if db.Mode == "stateful" && len(statement.Cases) > 0 {
			problem(statement.ID, "有状态场景不能使用静态查询 case")
		}
	}
	for _, step := range body.StepBindings {
		if !steps[step.StepID] || files[step.SourcePath] == nil || step.API == "" {
			problem("step_bindings", "步骤/API/源码引用无效")
		}
		for _, id := range step.Statements {
			if !statements[id] {
				problem("step_bindings", "SQL 引用不存在")
			}
		}
	}
	for id := range steps {
		found := false
		for _, b := range body.StepBindings {
			if b.StepID == id {
				found = true
			}
		}
		if !found {
			problem(id, "步骤缺少 API/SQL 对应关系")
		}
	}
	for _, a := range body.APIExpectations {
		if !steps[a.StepID] || a.Method == "" || a.Path == "" || a.Status < 100 || a.Status > 599 {
			problem("api_expectations", "API 预期引用不完整")
		}
	}
	slotIDs := map[string]bool{}
	body.Replay.RequiresGeneration = false
	for _, slot := range db.GenerationSlots {
		table, ok := findTable(tables, slot.Table)
		if !ok || slot.ID == "" || slotIDs[slot.ID] || len(slot.Keys) == 0 || len(slot.Keys) > 100 || writtenTables[slot.Table] || db.Mode != "stateful" {
			problem("generation_slots", "缺口只能属于不可变参考表，且键/数量/ID 必须明确")
			continue
		}
		slotIDs[slot.ID] = true
		for column, value := range slot.Fixed {
			field, ok := table.Field(column)
			if !ok || mysqlv1.ValidateValue(field.Column, value) != nil {
				problem(slot.ID, "缺口固定字段不符合 DDL")
			}
		}
		if !slot.Materialized {
			body.Replay.RequiresGeneration = true
		}
	}
	bound := map[string]bool{}
	for _, b := range body.DataBindings {
		expected, ok := input.QA.Fixed[b.QAKey]
		if !ok {
			problem("data_bindings", "未知 QA 固定值引用")
			continue
		}
		bound[b.QAKey] = true
		value, found := initialValue(*db, b)
		if !found {
			for _, slot := range db.GenerationSlots {
				if slot.Table == b.Table {
					for _, key := range slot.Keys {
						if key == b.Key {
							value, found = slot.Fixed[b.Column]
						}
					}
				}
			}
		}
		actual, err := value.String()
		if !found || err != nil || actual != expected {
			problem(b.QAKey, "实体与 QA 固定值不一致")
		}
	}
	for key := range input.QA.Fixed {
		if !bound[key] {
			problem(key, "QA 固定值缺少实体字段绑定")
		}
	}
	if len(report.MissingInputs)+len(report.Ambiguities)+len(report.Diagnostics)+len(report.Unsupported) > 0 {
		return report, nil
	}
	if len(writtenTables) > 0 && len(body.Preview) == 0 {
		problem("preview", "涉及写入的场景必须提供依据代码推导的 SQL 操作预演")
		return report, nil
	}
	if len(body.Preview) > 0 {
		if err := preview(ctx, body); err != nil {
			problem("preview", err.Error())
			return report, nil
		}
	}
	report.Ready = true
	report.CompiledBody = mysqlv1.Encode(body)
	inputHash := sha256.Sum256(mysqlv1.Encode(input))
	bodyHash := sha256.Sum256(report.CompiledBody)
	report.InputDigest = hex.EncodeToString(inputHash[:])
	report.CompiledDigest = hex.EncodeToString(bodyHash[:])
	return report, nil
}
func initialValue(db DatabaseScenario, b DataBinding) (mysqlv1.Value, bool) {
	table, ok := findTable(db.Tables, b.Table)
	if !ok || len(table.PrimaryKey) != 1 {
		return mysqlv1.Value{}, false
	}
	for _, row := range db.Initial[b.Table] {
		key, _ := row[table.PrimaryKey[0]].String()
		if key == b.Key {
			value, ok := row[b.Column]
			return value, ok
		}
	}
	return mysqlv1.Value{}, false
}
func validateInitial(tables []Table, initial map[string][]Entity) error {
	for name, rows := range initial {
		table, ok := findTable(tables, name)
		if !ok {
			return fmt.Errorf("unknown initial table %s", name)
		}
		if len(rows) > 1000 {
			return fmt.Errorf("initial rows exceed bound")
		}
		keys := map[string]bool{}
		for _, row := range rows {
			if len(row) != len(table.Columns) {
				return fmt.Errorf("initial %s must specify every column", name)
			}
			for _, field := range table.Columns {
				value, ok := row[field.Name]
				if !ok {
					return fmt.Errorf("missing column %s", field.Name)
				}
				if err := mysqlv1.ValidateValue(field.Column, value); err != nil {
					return err
				}
				if field.Type == "VARCHAR" && !value.IsNull() {
					s, _ := value.String()
					if utf8.RuneCountInString(s) > field.MaxLength {
						return fmt.Errorf("VARCHAR too long")
					}
				}
			}
			for i, columns := range append([][]string{table.PrimaryKey}, table.Unique...) {
				parts := []mysqlv1.Value{}
				null := false
				for _, name := range columns {
					parts = append(parts, row[name])
					null = null || row[name].IsNull()
				}
				if null && i > 0 {
					continue
				}
				key := fmt.Sprint(i) + string(mysqlv1.Encode(parts))
				if keys[key] {
					return fmt.Errorf("duplicate key in %s", table.Name)
				}
				keys[key] = true
			}
		}
	}
	for _, table := range tables {
		for _, row := range initial[table.Name] {
			for _, fk := range table.ForeignKeys {
				found := false
				for _, other := range initial[fk.Table] {
					equal := true
					for i, name := range fk.Columns {
						a, b := row[name], other[fk.References[i]]
						equal = equal && reflect.DeepEqual(a, b)
					}
					found = found || equal
				}
				if !found {
					return fmt.Errorf("foreign key not satisfied in initial %s", table.Name)
				}
			}
		}
	}
	return nil
}
