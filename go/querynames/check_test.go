package querynames

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestQueryNamingBoundary(t *testing.T) {
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, body, want string
		count            int
	}{
		{"same SQL different names", `store.NamedQuery(db,"ReadAccount").Raw("SELECT cash FROM accounts WHERE id=1").Scan(&value); store.NamedQuery(db,"OtherReadAccount").Raw("SELECT cash FROM accounts WHERE id=2").Scan(&value)`, "duplicates the definition", 2},
		{"same model different names", `type Account struct{ID string}; var a Account; store.NamedQuery(db,"GetAccount").First(&a,"id=?",1); store.NamedQuery(db,"OtherAccount").First(&a,"id=?",2)`, "duplicates the definition", 2},
		{"different model scope", `type Account struct{ID string}; var a Account; store.NamedQuery(db.Where("NOT archived"),"ActiveAccount").First(&a,"id=?",1); store.NamedQuery(db.Where("archived"),"ArchivedAccount").First(&a,"id=?",2)`, "", 2},
		{"different SQL beyond text cap", `store.NamedQuery(db,"Short").Raw("SELECT cash FROM accounts").Scan(&value); store.NamedQuery(db,"Other").Raw("SELECT cash FROM accounts WHERE archived").Scan(&value)`, "", 2},
		{"distinct", `store.NamedQuery(db,"AccountCash").Raw("SELECT cash FROM accounts").Scan(&value); store.NamedQuery(db,"EscrowTotal").Raw("SELECT sum(balance) FROM pair_escrows").Scan(&value)`, "", 2},
		{"handwritten", `store.NamedQuery(db,"AccountCash").Raw("SELECT cash FROM accounts").Scan(&value)`, "generated named query method", 1},
		{"missing", `db.Raw("SELECT cash FROM accounts").Scan(&value)`, "needs a source-owned", 1},
		{"reused", `store.NamedQuery(db,"Shared").Exec("UPDATE accounts SET cash=0"); store.NamedQuery(db,"Shared").Exec("DELETE FROM orders")`, "already used", 2},
		{"decoy", `db.Where("id=?",store.NamedQuery(db,"Decoy")).Find(&value)`, "needs a source-owned", 1},
		{"lowercase", `store.NamedQuery(db,"signal").Exec("SELECT 1")`, "invalid source-owned", 1},
		{"literal selection", `name:="MinuteCheckpoint"; if value>0 {name="DailyCheckpoint"}; store.NamedQuery(db,name).Exec("SELECT 1")`, "duplicates the definition", 1},
		{"runtime name", `name:=string(rune(value)); store.NamedQuery(db,name).Exec("SELECT 1")`, "needs a source-owned", 1},
		{"overwritten context", `store.NamedQuery(db,"LostName").WithContext(context.Background()).Exec("SELECT 1")`, "needs a source-owned", 1},
		{"generated missing", `query.Pair.WithContext(context.Background()).Find()`, "needs a source-owned", 1},
		{"generated named", `query.Pair.WithContext(store.WithQueryName(context.Background(),"SnapshotListPair")).Find()`, "", 1},
		{"generated lowercase", `query.Pair.WithContext(store.WithQueryName(context.Background(),"snapshot")).Find()`, "invalid source-owned", 1},
	}

	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			t.Fatal(err)
		}
		write(name, string(data))
	}
	write("internal/store/store.go", `package store
 import("gorm.io/gorm";"context")
 func NamedQuery(db *gorm.DB,name string)*gorm.DB{return db}
 func WithQueryName(ctx context.Context,name string)context.Context{return ctx}
 `)
	write("internal/store/query/pair.go", `package query
 import "context"
 type IPairDo interface{WithContext(context.Context)IPairDo;Find()([]int,error)}
 var Pair IPairDo
 `)
	for i, tc := range cases {
		name := "storage_queries.gen.go"
		if tc.name == "handwritten" {
			name = "queries.go"
		}
		write(fmt.Sprintf("internal/example%d/%s", i, name), `package example
 import("gorm.io/gorm";"context";"github.com/zfogg/gotel/go/internal/store";"github.com/zfogg/gotel/go/internal/store/query")
 var _=store.NamedQuery;var _=context.Background;var _=query.Pair
 func queries(db *gorm.DB){var value int64;_ = value;`+tc.body+`}
 `)
	}
	config := ratioFixtureConfig()
	config.Context = t.Context()
	pkgs, err := packages.Load(&packages.Config{Dir: root, Context: t.Context(), Mode: packages.LoadSyntax}, "./internal/example...")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*packages.Package{}
	for _, pkg := range pkgs {
		byPath[pkg.PkgPath] = pkg
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkg := byPath[fmt.Sprintf("github.com/zfogg/gotel/go/internal/example%d", i)]
			if pkg == nil {
				t.Fatal("fixture package missing")
			}
			count, errors := (&checker{config: config}).checkPackages([]*packages.Package{pkg})
			if count != tc.count {
				t.Fatalf("query count=%d, want %d: %v", count, tc.count, errors)
			}
			if tc.want == "" {
				if len(errors) != 0 {
					t.Fatal(errors)
				}
				return
			}
			for _, err := range errors {
				if strings.Contains(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("wanted %q, got %v", tc.want, errors)
		})
	}
}

func ratioFixtureConfig() Config {
	c := DefaultConfig()
	c.Patterns = []string{"./internal/...", "./cmd/..."}
	c.Exclude = append(c.Exclude, "/internal/store/query/", "/internal/testutil/", "/cmd/gen/")
	c.DefinitionFiles = []string{"storage_queries.gen.go"}
	c.RequireDefinitions, c.PascalCase = true, true
	c.Receivers = append(c.Receivers, Receiver{Type: Type{"github.com/zfogg/gotel/go/internal/store/query", ""}, Methods: c.Receivers[0].Methods, SQLArgument: -1, ContextArgument: -1})
	c.NamingFunctions = append(c.NamingFunctions, NamingFunction{"github.com/zfogg/gotel/go/internal/store", "NamedQuery", 1, 0, false}, NamingFunction{"github.com/zfogg/gotel/go/internal/store", "WithQueryName", 1, 0, true})
	return c
}
