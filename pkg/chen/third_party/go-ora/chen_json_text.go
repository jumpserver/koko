package go_ora

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Oracle supports defining native JSON as a BLOB containing UTF-8 JSON text.
// This asks the server to serialize the original cursor's values; it neither
// rewrites nor re-executes the submitted SELECT and does not decode OSON.
const chenJSONTextLimit = 16 << 20

func chenJSONTextNegotiation(nego *DataTypeNego) {
	if len(nego.Server.ServerCompileTimeCaps) <= 7 || nego.Server.ServerCompileTimeCaps[7] < 16 {
		return
	}
	// Opt in only to the Oracle 21.1 field version, intersected with the
	// server. Earlier servers and ordinary upstream mode retain version 11.
	nego.CompileTimeCaps[7] = 16
	// Oracle's thin drivers declare native JSON's universal representation.
	for index := 1; index < int(nego.TypeAndRep[0]); {
		if nego.TypeAndRep[index+1] == 0 {
			index += 2
			continue
		}
		if nego.TypeAndRep[index] == int16(JSON) {
			nego.TypeAndRep[index+2] = TNS_TYPE_REP_UNIVERSAL
			return
		}
		index += 4
	}
}

func (stmt *defaultStmt) chenJSONTextEnabled() bool {
	return stmt.stmtType == SELECT && stmt.connection.connOption.JSONAsText && stmt.connection.dBVersion != nil && stmt.connection.dBVersion.MajorVersion >= 21
}

func chenJSONTextValue(lob Lob, ctx context.Context, remaining int64) (driver.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	} // Legacy non-Context API only.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(lob.sourceLocator) == 0 {
		return nil, nil
	}
	size, err := lob.getSize()
	if err != nil {
		return nil, err
	}
	if size <= 0 || size > chenJSONTextLimit || size > remaining {
		return nil, errors.New("native JSON text exceeds the 16 MiB batch bound or is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := lob.getDataWithActiveContext(0, size)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size || !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("native JSON server text is incomplete or invalid UTF-8 JSON")
	}
	return string(data), nil
}

func (stmt *defaultStmt) chenJSONPrefetched() bool {
	return stmt.chenJSONTextEnabled() && stmt._hasJSON
}

func chenJSONInitialFetchReady(resultSet *ResultSet) bool {
	if len(resultSet.rows) == 0 {
		return false
	}
	source, ok := resultSet.parent.(interface{ chenJSONPrefetched() bool })
	return ok && source.chenJSONPrefetched()
}
