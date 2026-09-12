package main

import (
	"context"
	"encoding/json"
	"strings"
)

func clearTransaction(c *connection) {
	c.active = false
	c.writes = map[string]map[string]Entity{}
	c.base = map[string]uint64{}
}
func (s *state) commit(c *connection) error {
	for identity, version := range c.base {
		parts := strings.SplitN(identity, "\x00", 2)
		if s.committed[parts[0]][parts[1]].version != version {
			clearTransaction(c)
			return dbError(1213, "40001", "concurrent write conflict")
		}
	}
	view := s.view(c)
	if err := validateInitial(s.tables, asInitial(view)); err != nil {
		clearTransaction(c)
		return dbError(1213, "40001", "concurrent constraint conflict: "+err.Error())
	}
	s.version++
	for table, rows := range c.writes {
		for key, row := range rows {
			s.committed[table][key] = recordVersion{cloneRow(row), s.version}
		}
	}
	clearTransaction(c)
	return nil
}
func (s *state) control(ctx context.Context, id, sql string) (json.RawMessage, bool, error) {
	command := strings.TrimSuffix(strings.ToUpper(strings.Join(strings.Fields(sql), " ")), ";")
	compact := strings.ReplaceAll(command, " ", "")
	known := command == "BEGIN" || command == "START TRANSACTION" || command == "COMMIT" || command == "ROLLBACK" || strings.HasPrefix(compact, "SETAUTOCOMMIT=") || strings.HasPrefix(compact, "SETSESSIONAUTOCOMMIT=") || strings.HasPrefix(command, "SET SESSION TRANSACTION ISOLATION LEVEL ") || strings.HasPrefix(command, "SAVEPOINT ") || strings.HasPrefix(command, "ROLLBACK TO ")
	if !known {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	c := s.conn(id)
	switch {
	case command == "BEGIN" || command == "START TRANSACTION":
		if c.active {
			return nil, true, dbError(1235, "42000", "nested BEGIN unsupported")
		}
		c.active = true
	case command == "COMMIT":
		if err := s.commit(c); err != nil {
			return nil, true, err
		}
	case command == "ROLLBACK":
		clearTransaction(c)
	case strings.HasPrefix(command, "SET SESSION TRANSACTION ISOLATION LEVEL "):
		if command != "SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED" {
			return nil, true, dbError(1235, "42000", "only READ COMMITTED supported")
		}
	case strings.Contains(compact, "AUTOCOMMIT="):
		value := strings.SplitN(compact, "=", 2)[1]
		if value == "1" || value == "ON" {
			if !c.autocommit {
				if err := s.commit(c); err != nil {
					return nil, true, err
				}
			}
			c.autocommit = true
		} else if value == "0" || value == "OFF" {
			c.autocommit = false
		} else {
			return nil, true, dbError(1235, "42000", "invalid autocommit value")
		}
	default:
		return nil, true, dbError(1235, "42000", "transaction operation unsupported")
	}
	return okResult(c, 0, 0), true, nil
}
func (s *state) closeConnection(id string) { s.mu.Lock(); delete(s.sessions, id); s.mu.Unlock() }
