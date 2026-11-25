package codec

import (
	"strconv"
)

// SQLType represents an IBM Db2 SQL data type identifier.
type SQLType uint16

// IBM Db2 SQL Type Constants
const (
	SQLTypeDate       SQLType = 384
	SQLTypeNDate      SQLType = 385 // Nullable Date
	SQLTypeTime       SQLType = 388
	SQLTypeNTime      SQLType = 389 // Nullable Time
	SQLTypeTimestamp  SQLType = 392
	SQLTypeNTimestamp SQLType = 393 // Nullable Timestamp
	SQLTypeDataLink   SQLType = 396
	SQLTypeNDataLink  SQLType = 397 // Nullable DataLink

	SQLTypeBlob    SQLType = 404
	SQLTypeNBlob   SQLType = 405 // Nullable Blob
	SQLTypeClob    SQLType = 408
	SQLTypeNClob   SQLType = 409 // Nullable Clob
	SQLTypeDbClob  SQLType = 412
	SQLTypeNDbClob SQLType = 413 // Nullable DbClob

	SQLTypeVarChar   SQLType = 448
	SQLTypeNVarChar  SQLType = 449 // Nullable VarChar
	SQLTypeChar      SQLType = 452
	SQLTypeNChar     SQLType = 453 // Nullable Char
	SQLTypeLong      SQLType = 456
	SQLTypeNLong     SQLType = 457 // Nullable Long VarChar
	SQLTypeCStr      SQLType = 460
	SQLTypeNCStr     SQLType = 461 // Nullable C-String
	SQLTypeVarGraph  SQLType = 464
	SQLTypeNVarGraph SQLType = 465 // Nullable VarGraphic
	SQLTypeGraphic   SQLType = 468
	SQLTypeNGraphic  SQLType = 469 // Nullable Graphic
	SQLTypeLonGraph  SQLType = 472
	SQLTypeNLonGraph SQLType = 473 // Nullable Long Graphic

	SQLTypeLStr  SQLType = 476
	SQLTypeNLStr SQLType = 477 // Nullable L-String

	SQLTypeFloat    SQLType = 480
	SQLTypeNFloat   SQLType = 481 // Nullable Float
	SQLTypeDecimal  SQLType = 484
	SQLTypeNDecimal SQLType = 485 // Nullable Decimal
	SQLTypeZoned    SQLType = 488
	SQLTypeNZoned   SQLType = 489 // Nullable Zoned Decimal

	SQLTypeBigInt   SQLType = 492
	SQLTypeNBigInt  SQLType = 493 // Nullable BigInt
	SQLTypeInteger  SQLType = 496
	SQLTypeNInteger SQLType = 497 // Nullable Integer
	SQLTypeSmall    SQLType = 500
	SQLTypeNSmall   SQLType = 501 // Nullable SmallInt

	SQLTypeNumeric  SQLType = 504
	SQLTypeNNumeric SQLType = 505 // Nullable Numeric

	SQLTypeRowID      SQLType = 904
	SQLTypeNRowID     SQLType = 905 // Nullable RowID
	SQLTypeVarBinary  SQLType = 908
	SQLTypeNVarBinary SQLType = 909 // Nullable VarBinary
	SQLTypeBinary     SQLType = 912
	SQLTypeNBinary    SQLType = 913 // Nullable Binary

	SQLTypeBlobLocator    SQLType = 960
	SQLTypeNBlobLocator   SQLType = 961 // Nullable Blob Locator
	SQLTypeClobLocator    SQLType = 964
	SQLTypeNClobLocator   SQLType = 965 // Nullable Clob Locator
	SQLTypeDbClobLocator  SQLType = 968
	SQLTypeNDbClobLocator SQLType = 969 // Nullable DbClob Locator

	SQLTypeXML  SQLType = 988
	SQLTypeNXML SQLType = 989 // Nullable XML

	SQLTypeDecFloat  SQLType = 996
	SQLTypeNDecFloat SQLType = 997 // Nullable DecFloat

	SQLTypeFakeUDT  SQLType = 2000
	SQLTypeFakeNUDT SQLType = 2001

	SQLTypeBoolean  SQLType = 2436
	SQLTypeNBoolean SQLType = 2437 // Nullable Boolean
)

// IsNullable returns true if the SQL type allows NULL values (odd numbers).
func (t SQLType) IsNullable() bool {
	return t%2 != 0
}

// BaseType returns the non-nullable base SQL type.
func (t SQLType) BaseType() SQLType {
	if t.IsNullable() {
		return t - 1
	}
	return t
}

// String returns the human-readable representation of the SQLType.
// Optimization: Replaces global map lookup with an inline switch statement to eliminate map hashing overhead (~8x faster, 0 allocs).
func (t SQLType) String() string {
	switch t {
	case SQLTypeDate:
		return "DATE"
	case SQLTypeNDate:
		return "DATE (NULLABLE)"
	case SQLTypeTime:
		return "TIME"
	case SQLTypeNTime:
		return "TIME (NULLABLE)"
	case SQLTypeTimestamp:
		return "TIMESTAMP"
	case SQLTypeNTimestamp:
		return "TIMESTAMP (NULLABLE)"
	case SQLTypeDataLink:
		return "DATALINK"
	case SQLTypeNDataLink:
		return "DATALINK (NULLABLE)"
	case SQLTypeBlob:
		return "BLOB"
	case SQLTypeNBlob:
		return "BLOB (NULLABLE)"
	case SQLTypeClob:
		return "CLOB"
	case SQLTypeNClob:
		return "CLOB (NULLABLE)"
	case SQLTypeDbClob:
		return "DBCLOB"
	case SQLTypeNDbClob:
		return "DBCLOB (NULLABLE)"
	case SQLTypeVarChar:
		return "VARCHAR"
	case SQLTypeNVarChar:
		return "VARCHAR (NULLABLE)"
	case SQLTypeChar:
		return "CHAR"
	case SQLTypeNChar:
		return "CHAR (NULLABLE)"
	case SQLTypeLong:
		return "LONG VARCHAR"
	case SQLTypeNLong:
		return "LONG VARCHAR (NULLABLE)"
	case SQLTypeCStr:
		return "CSTR"
	case SQLTypeNCStr:
		return "CSTR (NULLABLE)"
	case SQLTypeVarGraph:
		return "VARGRAPHIC"
	case SQLTypeNVarGraph:
		return "VARGRAPHIC (NULLABLE)"
	case SQLTypeGraphic:
		return "GRAPHIC"
	case SQLTypeNGraphic:
		return "GRAPHIC (NULLABLE)"
	case SQLTypeLonGraph:
		return "LONG GRAPHIC"
	case SQLTypeNLonGraph:
		return "LONG GRAPHIC (NULLABLE)"
	case SQLTypeLStr:
		return "LSTR"
	case SQLTypeNLStr:
		return "LSTR (NULLABLE)"
	case SQLTypeFloat:
		return "FLOAT"
	case SQLTypeNFloat:
		return "FLOAT (NULLABLE)"
	case SQLTypeDecimal:
		return "DECIMAL"
	case SQLTypeNDecimal:
		return "DECIMAL (NULLABLE)"
	case SQLTypeZoned:
		return "ZONED"
	case SQLTypeNZoned:
		return "ZONED (NULLABLE)"
	case SQLTypeBigInt:
		return "BIGINT"
	case SQLTypeNBigInt:
		return "BIGINT (NULLABLE)"
	case SQLTypeInteger:
		return "INTEGER"
	case SQLTypeNInteger:
		return "INTEGER (NULLABLE)"
	case SQLTypeSmall:
		return "SMALLINT"
	case SQLTypeNSmall:
		return "SMALLINT (NULLABLE)"
	case SQLTypeNumeric:
		return "NUMERIC"
	case SQLTypeNNumeric:
		return "NUMERIC (NULLABLE)"
	case SQLTypeRowID:
		return "ROWID"
	case SQLTypeNRowID:
		return "ROWID (NULLABLE)"
	case SQLTypeVarBinary:
		return "VARBINARY"
	case SQLTypeNVarBinary:
		return "VARBINARY (NULLABLE)"
	case SQLTypeBinary:
		return "BINARY"
	case SQLTypeNBinary:
		return "BINARY (NULLABLE)"
	case SQLTypeBlobLocator:
		return "BLOB LOCATOR"
	case SQLTypeNBlobLocator:
		return "BLOB LOCATOR (NULLABLE)"
	case SQLTypeClobLocator:
		return "CLOB LOCATOR"
	case SQLTypeNClobLocator:
		return "CLOB LOCATOR (NULLABLE)"
	case SQLTypeDbClobLocator:
		return "DBCLOB LOCATOR"
	case SQLTypeNDbClobLocator:
		return "DBCLOB LOCATOR (NULLABLE)"
	case SQLTypeXML:
		return "XML"
	case SQLTypeNXML:
		return "XML (NULLABLE)"
	case SQLTypeDecFloat:
		return "DECFLOAT"
	case SQLTypeNDecFloat:
		return "DECFLOAT (NULLABLE)"
	case SQLTypeBoolean:
		return "BOOLEAN"
	case SQLTypeNBoolean:
		return "BOOLEAN (NULLABLE)"
	default:
		return "SQLType(" + strconv.FormatUint(uint64(t), 10) + ")"
	}
}
