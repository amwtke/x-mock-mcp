package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/server"
	"github.com/go-mysql-org/go-mysql/stmt"
	"math"
	"strconv"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

type handler struct {
	ctx          context.Context
	id, database string
	dispatch     pluginapi.Dispatch
	conn         *server.Conn
	statements   map[*stmt.PreparedStmt]mysqlv1.Metadata
}

func mysqlError(err error) error {
	code := uint16(1105)
	state := "HY000"
	if pluginapi.Code(err) == "UNSUPPORTED" {
		code = 1235
		state = "42000"
	}
	return &mysql.MyError{Code: code, State: state, Message: err.Error()}
}
func (h *handler) call(op string, payload any) (json.RawMessage, error) {
	raw, err := h.dispatch(h.ctx, pluginapi.Request{ConnectionID: h.id, Operation: op, Payload: mysqlv1.Encode(payload)})
	if err != nil {
		return nil, mysqlError(err)
	}
	var kind struct {
		Kind string `json:"kind"`
	}
	if err = json.Unmarshal(raw, &kind); err != nil {
		return nil, mysqlError(err)
	}
	if kind.Kind == "error" {
		var e mysqlv1.Error
		if err = pluginapi.Decode(raw, &e); err != nil {
			return nil, mysqlError(err)
		}
		return nil, &mysql.MyError{Code: e.Number, State: e.SQLState, Message: e.Message}
	}
	return raw, nil
}
func (h *handler) setStatus(s mysqlv1.SessionStatus) {
	if h.conn == nil {
		return
	}
	h.conn.UnsetStatus(mysql.SERVER_STATUS_AUTOCOMMIT | mysql.SERVER_STATUS_IN_TRANS)
	if s.Autocommit {
		h.conn.SetStatus(mysql.SERVER_STATUS_AUTOCOMMIT)
	}
	if s.InTransaction {
		h.conn.SetStatus(mysql.SERVER_STATUS_IN_TRANS)
	}
}
func (h *handler) result(raw json.RawMessage, binary bool) (*mysql.Result, error) {
	var v struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, mysqlError(err)
	}
	switch v.Kind {
	case "rows":
		var rows mysqlv1.Rows
		if err := pluginapi.Decode(raw, &rows); err != nil {
			return nil, mysqlError(err)
		}
		rs, err := encodeResultset(rows, binary)
		if err != nil {
			return nil, mysqlError(err)
		}
		h.setStatus(rows.Status)
		return &mysql.Result{Resultset: rs}, nil
	case "ok":
		var ok mysqlv1.OK
		if err := pluginapi.Decode(raw, &ok); err != nil {
			return nil, mysqlError(err)
		}
		if err := ok.Validate(); err != nil {
			return nil, mysqlError(err)
		}
		affected, _ := strconv.ParseUint(ok.AffectedRows, 10, 64)
		insert, _ := strconv.ParseUint(ok.LastInsertID, 10, 64)
		h.setStatus(ok.Status)
		return &mysql.Result{AffectedRows: affected, InsertId: insert, Warnings: ok.Warnings}, nil
	default:
		return nil, mysqlError(fmt.Errorf("invalid result kind %q", v.Kind))
	}
}
func (h *handler) UseDB(db string) error {
	raw, err := h.call("database.use", map[string]string{"database": db})
	if err != nil {
		return err
	}
	if _, err = h.result(raw, false); err == nil {
		h.database = db
	}
	return err
}
func (h *handler) HandleQuery(sql string) (*mysql.Result, error) {
	raw, err := h.call("query", mysqlv1.Query{Database: h.database, SQL: sql})
	if err != nil {
		return nil, err
	}
	return h.result(raw, false)
}
func (h *handler) HandleFieldList(string, string) ([]*mysql.Field, error) {
	return nil, mysqlError(pluginapi.Fail("UNSUPPORTED", "COM_FIELD_LIST unsupported"))
}
func (h *handler) HandleOtherCommand(byte, []byte) error {
	return mysqlError(pluginapi.Fail("UNSUPPORTED", "unsupported MySQL command"))
}
func (h *handler) HandleStmtPrepare(sql string) (int, int, any, error) {
	raw, err := h.call("statement.prepare", mysqlv1.Query{Database: h.database, SQL: sql})
	if err != nil {
		return 0, 0, nil, err
	}
	var meta mysqlv1.Metadata
	if err = pluginapi.Decode(raw, &meta); err != nil {
		return 0, 0, nil, mysqlError(err)
	}
	if meta.StatementID == "" {
		return 0, 0, nil, mysqlError(fmt.Errorf("missing statement id"))
	}
	prepared := &stmt.PreparedStmt{Params: len(meta.Parameters), Columns: len(meta.Columns), RawParamFields: [][]byte{}, RawColumnFields: [][]byte{}}
	for _, col := range meta.Parameters {
		f, e := wireField(col)
		if e != nil {
			return 0, 0, nil, mysqlError(e)
		}
		prepared.RawParamFields = append(prepared.RawParamFields, f.Dump())
	}
	for _, col := range meta.Columns {
		f, e := wireField(col)
		if e != nil {
			return 0, 0, nil, mysqlError(e)
		}
		prepared.RawColumnFields = append(prepared.RawColumnFields, f.Dump())
	}
	if len(h.statements) >= 256 {
		return 0, 0, nil, mysqlError(fmt.Errorf("prepared statement limit"))
	}
	h.statements[prepared] = meta
	return prepared.Params, prepared.Columns, prepared, nil
}
func parameter(col mysqlv1.Column, arg any) (mysqlv1.Value, error) {
	if arg == nil {
		v := mysqlv1.Null(col.Type)
		return v, mysqlv1.ValidateValue(col, v)
	}
	var value mysqlv1.Value
	switch col.Type {
	case "BIGINT":
		var n int64
		switch v := arg.(type) {
		case int64:
			n = v
		case int32:
			n = int64(v)
		case int16:
			n = int64(v)
		case int8:
			n = int64(v)
		case int:
			n = int64(v)
		case uint64:
			if v > math.MaxInt64 {
				return value, fmt.Errorf("BIGINT overflow")
			}
			n = int64(v)
		case uint32:
			n = int64(v)
		case uint16:
			n = int64(v)
		case uint8:
			n = int64(v)
		default:
			return value, fmt.Errorf("expected integer parameter, received %T", arg)
		}
		value = mysqlv1.Int(n)
	case "VARCHAR":
		switch v := arg.(type) {
		case mysql.TypedBytes:
			if v.Type != mysql.MYSQL_TYPE_VARCHAR && v.Type != mysql.MYSQL_TYPE_VAR_STRING && v.Type != mysql.MYSQL_TYPE_STRING {
				return value, fmt.Errorf("unsupported wire type for VARCHAR parameter")
			}
			value = mysqlv1.Text(string(v.Bytes))
		case string:
			value = mysqlv1.Text(v)
		case []byte:
			value = mysqlv1.Text(string(v))
		default:
			return value, fmt.Errorf("expected text parameter")
		}
	default:
		return value, fmt.Errorf("unsupported parameter type")
	}
	return value, mysqlv1.ValidateValue(col, value)
}
func (h *handler) HandleStmtExecute(contextValue any, sql string, args []any) (*mysql.Result, error) {
	ps, ok := contextValue.(*stmt.PreparedStmt)
	if !ok {
		return nil, mysqlError(fmt.Errorf("invalid statement context"))
	}
	meta, ok := h.statements[ps]
	if !ok || len(args) != len(meta.Parameters) {
		return nil, mysqlError(fmt.Errorf("statement ownership or arity mismatch"))
	}
	params := make([]mysqlv1.Value, len(args))
	for i, arg := range args {
		v, err := parameter(meta.Parameters[i], arg)
		if err != nil {
			return nil, mysqlError(err)
		}
		params[i] = v
	}
	raw, err := h.call("statement.execute", mysqlv1.Query{Database: h.database, SQL: sql, StatementID: meta.StatementID, Params: params})
	if err != nil {
		return nil, err
	}
	return h.result(raw, true)
}
func (h *handler) HandleStmtClose(contextValue any) error {
	ps, ok := contextValue.(*stmt.PreparedStmt)
	if !ok {
		return mysqlError(fmt.Errorf("invalid statement context"))
	}
	meta, ok := h.statements[ps]
	if !ok {
		return mysqlError(fmt.Errorf("unknown statement"))
	}
	delete(h.statements, ps)
	_, err := h.call("statement.close", map[string]string{"statement_id": meta.StatementID})
	return err
}
