package audit

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/m2sound2456/staffdisplay/backend/migrations"
)

// TestActionPatternMatchesDatabaseConstraint keeps the Go validator and the
// audit_logs CHECK constraint in sync; a divergence would let a row fail at
// insert time instead of at validation time.
func TestActionPatternMatchesDatabaseConstraint(t *testing.T) {
	content, err := fs.ReadFile(migrations.FS, "0004_create_audit_logs.sql")
	if err != nil {
		t.Fatalf("read 0004_create_audit_logs.sql: %v", err)
	}
	sql := string(content)

	if !strings.Contains(sql, "'"+ActionPattern+"'") {
		t.Errorf("0004_create_audit_logs.sql must enforce the action pattern %q", ActionPattern)
	}
	if !strings.Contains(sql, "actor_type IN ('user', 'device', 'system')") {
		t.Error("0004_create_audit_logs.sql must enforce the documented actor types")
	}
	for _, actorType := range AllActorTypes() {
		if !strings.Contains(sql, "'"+string(actorType)+"'") {
			t.Errorf("0004_create_audit_logs.sql must allow the actor type %q", actorType)
		}
	}
	if !regexp.MustCompile(`char_length\(action\) BETWEEN 3 AND 120`).MatchString(sql) {
		t.Error("0004_create_audit_logs.sql must bound the action length like the domain does")
	}
}
