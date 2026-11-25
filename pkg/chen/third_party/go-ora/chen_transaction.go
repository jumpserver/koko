package go_ora

// ChenSetAutoCommit changes only the driver's implicit commit flag. Chen uses
// this on its exclusively owned physical connection while staging grid edits
// or plan-table changes inside an existing server transaction. No transaction
// is started, committed or rolled back by this method.
func (conn *Connection) ChenSetAutoCommit(enabled bool) bool {
	previous := conn.autoCommit
	conn.autoCommit = enabled
	return previous
}
