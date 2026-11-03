package querynames

import (
	"strings"
	"testing"

	sequel "github.com/zfogg/sequel/go"
)

func TestStaticDefinitionsPreserveLiteralAndBindingSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		equal             bool
	}{
		{"sqlc metadata", "-- name: One :one\nSELECT id FROM alerts WHERE key=$1", "-- name: Two :one\nSELECT id FROM alerts WHERE key=$1", true},
		{"outer whitespace", "\r\n SELECT 1 \n", "SELECT 1", true},
		{"fill vs dynamic alert", "SELECT last_fill_ts FROM notified_fills WHERE key='fill' ORDER BY id DESC LIMIT 1", "SELECT last_fill_ts FROM notified_fills WHERE key=$1 ORDER BY id DESC LIMIT 1", false},
		{"complete vs fail", "UPDATE runs SET status='completed',error_message='' WHERE id=$1", "UPDATE runs SET status='failed',error_message=$1 WHERE id=$2", false},
		{"bar vs signal storage", "SELECT(pg_stat_get_live_tuples('public.bars'::regclass)*256)::bigint", "SELECT(pg_stat_get_live_tuples('public.signals'::regclass)*4096)::bigint", false},
		{"literal whitespace", "SELECT 'two spaces'", "SELECT 'two  spaces'", false},
		{"literal newline directive", "SELECT $$\n-- name: One :one\n$$", "SELECT $$\n-- name: Two :one\n$$", false},
		{"repeated marker", "SELECT $1,$1", "SELECT $1,$2", false},
		{"marker spelling", "SELECT $1", "SELECT $2", false},
		{"semantic hint", "SELECT /*+ hint_one */ id FROM items", "SELECT /*+ hint_two */ id FROM items", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left, _, err := sqlDefinitionIdentity(tc.left, sequel.DialectPostgreSQL, DefinitionExact)
			if err != nil {
				t.Fatal(err)
			}
			right, _, err := sqlDefinitionIdentity(tc.right, sequel.DialectPostgreSQL, DefinitionExact)
			if err != nil {
				t.Fatal(err)
			}
			if (left == right) != tc.equal {
				t.Fatalf("definition equality=%t want=%t", left == right, tc.equal)
			}
			if strings.Contains(left, "fill") || strings.Contains(left, "SELECT") {
				t.Fatal("definition key retained source text")
			}
		})
	}
	ck := checker{config: Config{DefinitionIdentity: DefinitionShape}}
	if ck.queryIdentity("SELECT 1") != ck.queryIdentity("SELECT 2") {
		t.Fatal("explicit shape policy lost")
	}
}
