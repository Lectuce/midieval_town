package storage

import (
	"context"
	"database/sql"
)

// *sql.DB 和 *sql.Tx 都实现这个接口。
// 事务内传 tx，普通查询传 db。

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)

	QueryContext(context.Context, string, ...any) (*sql.Rows, error)

	QueryRowContext(context.Context, string, ...any) *sql.Row
}
