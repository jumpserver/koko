/*
 * Copyright (c) 2000-2018, 达梦数据库有限公司.
 * All rights reserved.
 */
package dm

import "strings"

type DmResult struct {
	filterable
	dmStmt       *DmStatement
	affectedRows int64
	insertId     int64
}

func newDmResult(bs *DmStatement, execInfo *execRetInfo) *DmResult {
	result := DmResult{}
	result.resetFilterable(&bs.filterable)
	result.dmStmt = bs
	result.affectedRows = execInfo.updateCount

	if execInfo.lastInsertId == 0 && execInfo.hasResultSet && strings.Index(execInfo.nativeSQL, "/*DMGORM-UPSERT*/") == 0 {
		if len(execInfo.rsDatas) > 0 && len(execInfo.rsDatas[0]) > 0 {
			result.insertId = Dm_build_943.Dm_build_1173(execInfo.rsDatas[0][1])
		} else {
			result.insertId = 0
		}
	} else {
		result.insertId = execInfo.lastInsertId
	}
	result.idGenerator = dmResultIDGenerator

	return &result
}

func (r *DmResult) LastInsertId() (int64, error) {

	if len(r.filterChain.filters) == 0 {
		return r.lastInsertId()
	}
	return r.filterChain.reset().DmResultLastInsertId(r)
}

func (r *DmResult) RowsAffected() (int64, error) {

	if len(r.filterChain.filters) == 0 {
		return r.rowsAffected()
	}
	return r.filterChain.reset().DmResultRowsAffected(r)
}

func (result *DmResult) lastInsertId() (int64, error) {
	return result.insertId, nil
}

func (result *DmResult) rowsAffected() (int64, error) {
	return result.affectedRows, nil
}
