package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

func extraDefaultSchema(ctx context.Context, conn *sql.Conn, d dialect.ID, database string) (string, error) {
	var query string
	switch d {
	case dialect.SQLServer:
		query = "SELECT COALESCE(SCHEMA_NAME(), 'dbo')"
	case dialect.DB2:
		query = "SELECT RTRIM(CURRENT SCHEMA) FROM SYSIBM.SYSDUMMY1"
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		query = "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM DUAL"
	case dialect.ClickHouse:
		return database, nil
	default:
		return "", ErrUnsupportedDialect
	}
	var schema string
	if err := conn.QueryRowContext(ctx, query).Scan(&schema); err != nil {
		return "", fmt.Errorf("read catalog schema: %w", err)
	}
	return schema, nil
}

func extraMetadata(ctx context.Context, conn *sql.Conn, d dialect.ID, req MetadataRequest) ([]Relation, error) {
	items := []Relation{}
	if err := validateCatalogInput(conn, d, req.Context.Database, req.Context.Schema, req.Prefix); err != nil {
		return items, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	if err := catalogDatabaseAccess(ctx, conn, d, req.Context.Database); err != nil {
		return items, err
	}
	schema := req.Context.Schema
	if schema == "" {
		var err error
		schema, err = extraDefaultSchema(ctx, conn, d, req.Context.Database)
		if err != nil {
			return items, err
		}
	}
	if d == dialect.ClickHouse && schema != req.Context.Database {
		return items, ErrCatalogForbidden
	}
	var clickHouse *clickHouseCapabilities
	if d == dialect.ClickHouse {
		var err error
		clickHouse, err = probeClickHouseCapabilities(ctx, conn, "objects")
		if err != nil {
			return items, err
		}
	}
	query, err := extraObjectQuery(d, CatalogObjectRequest{Database: req.Context.Database, Schema: schema, Query: req.Prefix}, true, req.Limit, clickHouse)
	if err != nil {
		return items, err
	}
	rows, err := queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
	if err != nil {
		return items, fmt.Errorf("list relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item Relation
		var objectType string
		var comment sql.NullString
		if err := rows.Scan(&item.Database, &item.Schema, &item.Name, &item.Kind, &objectType, &comment); err != nil {
			return nil, err
		}
		item.Comment = comment.String
		if item.Kind == "view" {
			item.Kind = "VIEW"
		} else {
			item.Kind = "BASE TABLE"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func extraColumns(ctx context.Context, conn *sql.Conn, d dialect.ID, req ColumnRequest) ([]Column, error) {
	items := []Column{}
	if err := validateCatalogInput(conn, d, req.Context.Database, req.Schema, req.Table, req.Context.Schema); err != nil {
		return items, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	if err := catalogDatabaseAccess(ctx, conn, d, req.Context.Database); err != nil {
		return items, err
	}
	schema := req.Schema
	if schema == "" {
		schema = req.Context.Schema
	}
	if schema == "" {
		var err error
		schema, err = extraDefaultSchema(ctx, conn, d, req.Context.Database)
		if err != nil {
			return items, err
		}
	}
	if d == dialect.ClickHouse && schema != req.Context.Database {
		return items, ErrCatalogForbidden
	}
	if err := CatalogObjectAccess(ctx, conn, d, schema, req.Table); err != nil {
		// SQL Server resolves unqualified tables in the default schema, then
		// dbo. Explicit schema qualifiers must never take this fallback.
		if d != dialect.SQLServer || req.Schema != "" || schema == "dbo" || !errors.Is(err, ErrCatalogNotFound) {
			return items, err
		}
		schema = "dbo"
		if err := CatalogObjectAccess(ctx, conn, d, schema, req.Table); err != nil {
			return items, err
		}
	}
	query, err := extraColumnQuery(d, CatalogColumnRequest{Database: req.Context.Database, Schema: schema, Table: req.Table}, 2000)
	if err != nil {
		return items, err
	}
	rows, err := queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
	if err != nil {
		return items, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item Column
		var database, owner, table string
		var ordinal int
		var comment sql.NullString
		if err := rows.Scan(&database, &owner, &table, &item.Name, &item.DataType, &item.Nullable, &comment, &ordinal); err != nil {
			return nil, err
		}
		item.Comment = comment.String
		item.DataType = strings.TrimSpace(item.DataType)
		items = append(items, item)
	}
	return items, rows.Err()
}
