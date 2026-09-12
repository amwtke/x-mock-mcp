package main

import (
	"context"
	"fmt"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func verifyState(s *state, spec VerificationSpec, counts map[string]int) pluginapi.Verification {
	report := pluginapi.Verification{Passed: true, Issues: []pluginapi.Failure{}}
	issue := func(message string) {
		report.Passed = false
		report.Issues = append(report.Issues, pluginapi.Failure{Code: "EXPECTATION_FAILED", Message: message})
	}
	for id, expected := range spec.ExpectCalls {
		if counts[id] < expected.Min || counts[id] > expected.Max {
			issue(fmt.Sprintf("%s called %d times, expected %d..%d", id, counts[id], expected.Min, expected.Max))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.view(nil)
	for _, expected := range spec.FinalState {
		count := 0
		for _, row := range view[expected.Table] {
			match := true
			for name, value := range expected.Where {
				match = match && equalValue(row[name], value)
			}
			if !match {
				continue
			}
			count++
			for name, value := range expected.Fields {
				if !equalValue(row[name], value) {
					issue(fmt.Sprintf("%s.%s differs from QA state expectation", expected.Table, name))
				}
			}
		}
		if count != expected.Count {
			issue(fmt.Sprintf("%s matched %d rows, expected %d", expected.Table, count, expected.Count))
		}
	}
	for _, c := range s.sessions {
		if len(c.base) > 0 {
			issue("application left uncommitted writes")
		}
	}
	return report
}
func (p *plugin) Verify(context.Context) (pluginapi.Verification, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.store == nil {
		return pluginapi.Verification{}, pluginapi.Fail("STATE_CONFLICT", "right not started")
	}
	report := verifyState(p.store, p.bundle.Verification, p.counts)
	if len(p.violations) > 0 {
		report.Passed = false
		report.Issues = append(report.Issues, p.violations...)
	}
	return report, nil
}
func preview(ctx context.Context, body Bundle) error {
	db := body.DatabaseScenario
	db.Initial = map[string][]Entity{}
	for table, rows := range body.DatabaseScenario.Initial {
		for _, row := range rows {
			db.Initial[table] = append(db.Initial[table], cloneRow(row))
		}
	}
	for _, slot := range db.GenerationSlots {
		if slot.Materialized {
			continue
		}
		table, _ := findTable(db.Tables, slot.Table)
		for _, key := range slot.Keys {
			row := cloneRow(Entity(slot.Fixed))
			field, _ := table.Field(table.PrimaryKey[0])
			row[field.Name] = mysqlv1.Value{Type: field.Type, Value: mysqlv1.Encode(key)}
			if len(row) != len(table.Columns) {
				return fmt.Errorf("preview requires explicit values for all reference fields in slot %s", slot.ID)
			}
			db.Initial[table.Name] = append(db.Initial[table.Name], row)
		}
	}
	s, err := newState(db)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, step := range body.Preview {
		if step.Connection == "" || (step.Control == "") == (step.StatementID == "") {
			return fmt.Errorf("preview step requires connection and exactly one operation")
		}
		if step.Control != "" {
			if _, known, err := s.control(ctx, step.Connection, step.Control); err != nil || !known {
				return fmt.Errorf("preview control failed: %v", err)
			}
			continue
		}
		var selected *Statement
		for i := range db.Statements {
			if db.Statements[i].ID == step.StatementID {
				selected = &db.Statements[i]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("preview references unknown SQL rule")
		}
		_, params, err := matchStatement(db, selected.SQL, step.Params, false)
		if err != nil {
			return err
		}
		if _, err = s.execute(ctx, step.Connection, selected.Plan, params); err != nil {
			return err
		}
		counts[step.StatementID]++
	}
	report := verifyState(s, body.Verification, counts)
	if !report.Passed {
		return fmt.Errorf("preview state assertions failed: %v", report.Issues)
	}
	return nil
}
