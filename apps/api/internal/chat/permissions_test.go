package chat

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sayem314/oracle/apps/api/internal/permission"
)

// TestSetPermissionsConcurrent hammers SetPermissions from one goroutine while
// another reads, proving the ruleset swap is race-free.
func TestSetPermissionsConcurrent(t *testing.T) {
	e := &Engine{Permissions: permission.NewRuleset(permission.Ask, nil)}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			e.SetPermissions(permission.NewRuleset(permission.Allow, nil))
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			rules := e.currentPermissions()
			assert.NotNil(t, rules)
		}
	}()
	wg.Wait()

	assert.Equal(t, permission.Allow, e.currentPermissions().Default)
}

// TestAsHeadlessSharesPermissions proves a headless copy observes a ruleset
// save made on the interactive engine.
func TestAsHeadlessSharesPermissions(t *testing.T) {
	e := &Engine{Permissions: permission.NewRuleset(permission.Ask, nil)}
	headless := e.AsHeadless()

	e.SetPermissions(permission.NewRuleset(permission.Allow, nil))

	assert.Equal(t, permission.Allow, headless.currentPermissions().Default)
}
