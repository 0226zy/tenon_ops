package store

import (
	"strings"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	script := `-- a comment with ; inside
CREATE TABLE a (id INT);
/* block ; comment */
INSERT INTO a VALUES ('x;y'), ("z;w");
-- trailing
`
	stmts, err := SplitSQL(script)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("got %d statements: %#v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "CREATE TABLE a (id INT)") {
		t.Fatalf("first: %q", stmts[0])
	}
	if !strings.Contains(stmts[1], `INSERT INTO a VALUES ('x;y'), ("z;w")`) {
		t.Fatalf("second: %q", stmts[1])
	}
}

func TestIdentityAndLimits(t *testing.T) {
	if Limit(0, 50, 100) != 50 || Limit(200, 50, 100) != 100 || Limit(10, 50, 100) != 10 {
		t.Fatal("Limit")
	}
	if quoteIdent("t_user") != "`t_user`" {
		t.Fatal("quoteIdent")
	}
}
