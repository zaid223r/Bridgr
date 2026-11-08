package brorm

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/zaid223r/Bridgr/core/brerrors"
)

type listTodo struct {
	ID       uint   `json:"id" gorm:"primaryKey"`
	Title    string `json:"title"`
	OwnerID  int    `json:"ownerId"`
	Archived bool   `json:"archived" gorm:"column:is_archived"`
	Body     string `json:"body"`
}

func (listTodo) FilterableFields() []string { return []string{"title", "ownerId", "archived"} }

// dryRunDB returns a GORM handle that builds SQL without a database, plus a
// pointer to the last query it built.
func dryRunDB(t *testing.T) (*gorm.DB, *string) {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=invalid"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var last string
	err = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
		last = tx.Statement.SQL.String()
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, &last
}

func TestListFiltersUseColumns(t *testing.T) {
	db, last := dryRunDB(t)
	m := &GormModel[listTodo]{DB: db}

	_, err := m.List(map[string][]string{
		"title":    {"milk"},
		"ownerId":  {"3"},
		"archived": {"false"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"is_archived = $", "owner_id = $", "title LIKE $"} {
		if !strings.Contains(*last, want) {
			t.Errorf("SQL %q does not contain %q", *last, want)
		}
	}
	if strings.Contains(*last, "ownerId") {
		t.Errorf("SQL %q uses the JSON name instead of the column", *last)
	}
}

func TestListRejectsUnknownFilters(t *testing.T) {
	db, _ := dryRunDB(t)
	m := &GormModel[listTodo]{DB: db}

	_, err := m.List(map[string][]string{"body": {"x"}, "title": {"y"}, "id; DROP TABLE x": {"1"}})
	fieldErr, ok := errors.AsType[*brerrors.InvalidFilterFieldError](err)
	if !ok {
		t.Fatalf("err = %v, want InvalidFilterFieldError", err)
	}
	if want := []string{"body", "id; DROP TABLE x"}; !slices.Equal(fieldErr.Fields, want) {
		t.Errorf("invalid fields = %v, want %v (sorted)", fieldErr.Fields, want)
	}
}
