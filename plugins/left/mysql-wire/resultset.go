package main

import (
	"encoding/binary"
	"fmt"
	"github.com/go-mysql-org/go-mysql/mysql"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func wireField(column mysqlv1.Column) (*mysql.Field, error) {
	field := &mysql.Field{Name: []byte(column.Name), OrgName: []byte(column.Name)}
	if !column.Nullable {
		field.Flag |= mysql.NOT_NULL_FLAG
	}
	switch column.Type {
	case "BIGINT":
		field.Type = mysql.MYSQL_TYPE_LONGLONG
		field.Charset = 63
		field.ColumnLength = 20
	case "VARCHAR":
		field.Type = mysql.MYSQL_TYPE_VAR_STRING
		field.Charset = 46
		field.ColumnLength = 1024
	default:
		return nil, fmt.Errorf("unsupported MySQL column type %s", column.Type)
	}
	return field, nil
}
func encodeResultset(rows mysqlv1.Rows, binaryMode bool) (*mysql.Resultset, error) {
	if err := mysqlv1.ValidateRows(mysqlv1.Metadata{Columns: rows.Columns}, rows); err != nil {
		return nil, err
	}
	result := mysql.NewResultset(len(rows.Columns))
	for i, column := range rows.Columns {
		field, err := wireField(column)
		if err != nil {
			return nil, err
		}
		result.Fields[i] = field
		result.FieldNames[column.Name] = i
	}
	for _, values := range rows.Rows {
		row := []byte{}
		if binaryMode {
			row = make([]byte, 1+(len(rows.Columns)+9)/8)
		}
		for i, raw := range values {
			value := mysqlv1.Value{Type: rows.Columns[i].Type, Value: raw}
			if value.IsNull() {
				if binaryMode {
					row[1+(i+2)/8] |= 1 << uint((i+2)%8)
				} else {
					row = append(row, 0xfb)
				}
				continue
			}
			if binaryMode && value.Type == "BIGINT" {
				n, err := value.Int64()
				if err != nil {
					return nil, err
				}
				row = binary.LittleEndian.AppendUint64(row, uint64(n))
			} else {
				str, err := value.String()
				if err != nil {
					return nil, err
				}
				row = append(row, mysql.PutLengthEncodedString([]byte(str))...)
			}
		}
		result.RowDatas = append(result.RowDatas, mysql.RowData(row))
	}
	return result, nil
}
