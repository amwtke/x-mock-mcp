package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func (p *plugin) Export(ctx context.Context, spec pluginapi.ExportSpec) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var body Bundle
	if err := pluginapi.Decode(spec.Snapshot, &body); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(body, p.bundle) {
		return nil, pluginapi.Fail("STATE_CONFLICT", "export snapshot does not match active scenario")
	}
	if len(spec.Captures) == 0 {
		return nil, pluginapi.Invalid("select successful generated requests explicitly")
	}
	for _, capture := range spec.Captures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		confirmed := false
		digest := sha256.Sum256(capture.Candidate)
		hash := hex.EncodeToString(digest[:])
		for _, need := range p.pending {
			if reflect.DeepEqual(need.request, capture.Request) && need.candidateDigest == hash && need.err == nil && reflect.DeepEqual(need.result, capture.Response) {
				select {
				case <-need.done:
					confirmed = true
				default:
				}
			}
		}
		if !confirmed {
			return nil, pluginapi.Fail("INVALID_RESULT", "capture is not a confirmed successful generation")
		}
		var fill mysqlv1.EntityFill
		if err := pluginapi.Decode(capture.Candidate, &fill); err != nil {
			return nil, err
		}
		found := false
		for i, slot := range body.DatabaseScenario.GenerationSlots {
			if slot.ID != fill.SlotID {
				continue
			}
			found = true
			table, _ := findTable(body.DatabaseScenario.Tables, slot.Table)
			if len(fill.Rows) != len(slot.Keys) {
				return nil, pluginapi.Invalid("slot size mismatch")
			}
			keys := map[string]bool{}
			for _, key := range slot.Keys {
				keys[key] = true
			}
			for _, row := range fill.Rows {
				key, err := row[table.PrimaryKey[0]].String()
				if err != nil || !keys[key] {
					return nil, pluginapi.Invalid("slot key mismatch")
				}
				delete(keys, key)
				for name, value := range slot.Fixed {
					if !equalValue(row[name], value) {
						return nil, pluginapi.Fail("INVALID_RESULT", "export violates QA constraint")
					}
				}
				existing := false
				for _, old := range body.DatabaseScenario.Initial[slot.Table] {
					oldKey, _ := old[table.PrimaryKey[0]].String()
					if oldKey == key {
						existing = true
						if !reflect.DeepEqual(cloneRow(old), cloneRow(Entity(row))) {
							return nil, pluginapi.Fail("STATE_CONFLICT", "reference entity conflict")
						}
					}
				}
				if !existing {
					body.DatabaseScenario.Initial[slot.Table] = append(body.DatabaseScenario.Initial[slot.Table], cloneRow(Entity(row)))
				}
			}
			body.DatabaseScenario.GenerationSlots[i].Materialized = true
		}
		if !found {
			return nil, pluginapi.Invalid("capture references unknown slot")
		}
	}
	if err := validateInitial(body.DatabaseScenario.Tables, body.DatabaseScenario.Initial); err != nil {
		return nil, err
	}
	body.Replay.RequiresGeneration = false
	for _, slot := range body.DatabaseScenario.GenerationSlots {
		body.Replay.RequiresGeneration = body.Replay.RequiresGeneration || !slot.Materialized
	}
	return mysqlv1.Encode(body), nil
}
