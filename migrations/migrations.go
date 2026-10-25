// Package migrations embeds the tenon_ops MySQL schema.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

func InitSQL() (string, error) {
	b, err := FS.ReadFile("000001_init.mysql.sql")
	return string(b), err
}
