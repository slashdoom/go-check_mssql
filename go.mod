module check_mssql

go 1.23.6

replace example.org/config => ./config

replace example.org/logger => ./logger

require (
	github.com/denisenkom/go-mssqldb v0.12.3
	github.com/spf13/pflag v1.0.6
)

require (
	example.org/config v0.0.0-00010101000000-000000000000 // indirect
	example.org/logger v0.0.0-00010101000000-000000000000 // indirect
	github.com/golang-sql/civil v0.0.0-20190719163853-cb61b32ac6fe // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
)
